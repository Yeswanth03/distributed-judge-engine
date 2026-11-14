package handler

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/oauth"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/repository"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/token"
)

// OAuthStore is the part of the repository the Google and GitHub sign-in uses.
type OAuthStore interface {
	SaveOAuthState(ctx context.Context, stateHash []byte, s repository.OAuthState, expiresAt time.Time) error
	TakeOAuthState(ctx context.Context, stateHash []byte, now time.Time) (*repository.OAuthState, error)
	UserByIdentity(ctx context.Context, provider, subject string) (*model.User, error)
	LinkIdentity(ctx context.Context, userID uuid.UUID, provider, subject, email string) error
	CreateOAuthUser(ctx context.Context, u *model.User, provider, subject string) error
	SaveLoginCode(ctx context.Context, codeHash []byte, userID uuid.UUID, expiresAt time.Time) error
	TakeLoginCode(ctx context.Context, codeHash []byte, now time.Time) (uuid.UUID, error)
}

const (
	stateTTL     = 10 * time.Minute // time to get through the provider's consent page
	loginCodeTTL = time.Minute      // time for the page to collect its tokens
)

// Reasons a sign-in fails, sent back to the page as ?login_error=...
const (
	errCancelled        = "cancelled"          // the user declined at the provider
	errProvider         = "provider_error"     // the provider failed or answered strangely
	errEmailNotVerified = "email_not_verified" // the provider has no verified email for the account
	errEmailInUse       = "email_in_use"       // a password account has this email; sign in with the password
	errAccountDisabled  = "account_disabled"
)

var errSignIn = errors.New("sign-in refused")

type signInError struct{ code string }

func (e signInError) Error() string { return "sign-in refused: " + e.code }
func (e signInError) Unwrap() error { return errSignIn }

// oauthProviders lists the providers a page can offer buttons for.
func (h *Handler) oauthProviders(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(h.opts.Providers))
	for name := range h.opts.Providers {
		names = append(names, name)
	}
	slices.Sort(names)
	writeJSON(w, http.StatusOK, map[string]any{"providers": names})
}

// oauthStart sends the browser to the provider. redirect_uri is the page to come back to afterwards; only the
// configured origins are accepted, so this can't be used to bounce users to another site.
func (h *Handler) oauthStart(w http.ResponseWriter, r *http.Request) {
	p, ok := h.opts.Providers[r.PathValue("provider")]
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "unknown sign-in provider")
		return
	}
	back := r.URL.Query().Get("redirect_uri")
	if !h.allowedRedirect(back) {
		WriteError(w, http.StatusBadRequest, "bad_request", "redirect_uri is not an allowed page")
		return
	}
	state, stateHash, err := token.NewRefreshToken() // 256 random bits, stored only as a hash
	if err != nil {
		h.internalError(w, r, "make state", err)
		return
	}
	verifier := oauth2.GenerateVerifier()
	if err := h.oauth.SaveOAuthState(r.Context(), stateHash,
		repository.OAuthState{Provider: p.Name(), CodeVerifier: verifier, RedirectURI: back}, h.now().Add(stateTTL)); err != nil {
		h.internalError(w, r, "save sign-in", err)
		return
	}
	http.Redirect(w, r, p.AuthURL(state, verifier), http.StatusFound)
}

// oauthCallback is where the provider sends the browser back. It finishes the sign-in on the server and sends
// the browser to the page with a one-time login code, never with tokens.
func (h *Handler) oauthCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := h.opts.Providers[r.PathValue("provider")]
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "unknown sign-in provider")
		return
	}
	q := r.URL.Query()
	st, err := h.oauth.TakeOAuthState(r.Context(), token.HashRefreshToken(q.Get("state")), h.now())
	if errors.Is(err, repository.ErrNotFound) || (err == nil && st.Provider != p.Name()) {
		// Without a valid state there is no trusted page to go back to, so answer here.
		WriteError(w, http.StatusBadRequest, "invalid_state", "this sign-in expired or was already used; please start again")
		return
	}
	if err != nil {
		h.internalError(w, r, "take sign-in", err)
		return
	}
	if e := q.Get("error"); e != "" {
		code := errProvider
		if e == "access_denied" {
			code = errCancelled
		}
		redirectWith(w, r, st.RedirectURI, "login_error", code)
		return
	}

	id, err := p.Identify(r.Context(), q.Get("code"), st.CodeVerifier)
	if err != nil {
		h.log.WarnContext(r.Context(), "provider sign-in failed", "provider", p.Name(), "error", err)
		redirectWith(w, r, st.RedirectURI, "login_error", errProvider)
		return
	}
	u, err := h.resolveOAuthUser(r.Context(), p.Name(), id)
	var refused signInError
	if errors.As(err, &refused) {
		redirectWith(w, r, st.RedirectURI, "login_error", refused.code)
		return
	}
	if err != nil {
		h.internalError(w, r, "resolve user", err)
		return
	}
	if u.Status != model.StatusActive {
		redirectWith(w, r, st.RedirectURI, "login_error", errAccountDisabled)
		return
	}
	code, codeHash, err := token.NewRefreshToken()
	if err == nil {
		err = h.oauth.SaveLoginCode(r.Context(), codeHash, u.ID, h.now().Add(loginCodeTTL))
	}
	if err != nil {
		h.internalError(w, r, "save login code", err)
		return
	}
	h.log.InfoContext(r.Context(), "provider sign-in", "provider", p.Name(), "user_id", u.ID)
	redirectWith(w, r, st.RedirectURI, "login_code", code)
}

// resolveOAuthUser finds or creates the user for a provider account:
//  1. an account already linked to it;
//  2. else an account with the same email, but only when both sides have verified that email: never a password
//     account, whose email nobody has verified yet, or anyone could take it over by signing up at the provider
//     with that address;
//  3. else a new account, with no password.
func (h *Handler) resolveOAuthUser(ctx context.Context, provider string, id oauth.Identity) (*model.User, error) {
	u, err := h.oauth.UserByIdentity(ctx, provider, id.Subject)
	if err == nil || !errors.Is(err, repository.ErrNotFound) {
		return u, err
	}
	if id.Email == "" || !id.EmailVerified || !validEmail(id.Email) {
		return nil, signInError{errEmailNotVerified}
	}
	existing, err := h.store.GetUserByEmail(ctx, id.Email)
	switch {
	case err == nil && existing.EmailVerified:
		return existing, h.oauth.LinkIdentity(ctx, existing.ID, provider, id.Subject, id.Email)
	case err == nil:
		return nil, signInError{errEmailInUse}
	case !errors.Is(err, repository.ErrNotFound):
		return nil, err
	}

	base := usernameFrom(id.Username)
	name := strings.TrimSpace(id.Name)
	if name == "" || len([]rune(name)) > maxDisplayName {
		name = base
	}
	for attempt := 0; attempt < 5; attempt++ {
		username := base
		if attempt > 0 { // taken: add a short random suffix
			username = base[:min(len(base), 24)] + "_" + strconv.Itoa(1000+rand.IntN(9000))
		}
		u := &model.User{ID: uuid.New(), Email: id.Email, Username: username, DisplayName: name, Role: model.RoleUser,
			Status: model.StatusActive, EmailVerified: true, CreatedAt: h.now().UTC().Truncate(time.Microsecond)}
		err := h.oauth.CreateOAuthUser(ctx, u, provider, id.Subject)
		switch {
		case err == nil:
			return u, nil
		case errors.Is(err, repository.ErrUsernameTaken):
			continue
		case errors.Is(err, repository.ErrEmailTaken): // registered meanwhile by someone else's request
			return nil, signInError{errEmailInUse}
		default:
			return nil, err
		}
	}
	return nil, errors.New("no free username after 5 attempts")
}

var notUsernameChars = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// usernameFrom turns a provider's username or email local part into one of ours: 3 to 30 letters, digits or
// underscores.
func usernameFrom(s string) string {
	u := strings.Trim(notUsernameChars.ReplaceAllString(s, "_"), "_")
	if len(u) > 30 {
		u = u[:30]
	}
	for len(u) < 3 {
		u += "_user"
	}
	return u
}

type exchangeRequest struct {
	LoginCode string `json:"login_code"`
}

// oauthExchange swaps a login code (single use, one minute) for tokens, starting a session on this device.
func (h *Handler) oauthExchange(w http.ResponseWriter, r *http.Request) {
	var req exchangeRequest
	if !decode(w, r, &req) {
		return
	}
	userID, err := h.oauth.TakeLoginCode(r.Context(), token.HashRefreshToken(req.LoginCode), h.now())
	if errors.Is(err, repository.ErrNotFound) || req.LoginCode == "" {
		WriteError(w, http.StatusUnauthorized, "invalid_login_code", "this sign-in expired or was already used; please start again")
		return
	}
	if err != nil {
		h.internalError(w, r, "take login code", err)
		return
	}
	u, err := h.store.GetUserByID(r.Context(), userID)
	if err != nil {
		h.internalError(w, r, "get user", err)
		return
	}
	if u.Status != model.StatusActive {
		WriteError(w, http.StatusForbidden, "account_disabled", "this account is disabled")
		return
	}
	s, refresh, refreshHash, err := h.newSession(r, u.ID)
	if err == nil {
		err = h.store.CreateSession(r.Context(), s, refreshHash)
	}
	if err != nil {
		h.internalError(w, r, "start session", err)
		return
	}
	resp, err := h.issue(u, s, refresh, true)
	if err != nil {
		h.internalError(w, r, "issue token", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// allowedRedirect accepts an absolute http(s) URL whose origin is on the allow list.
func (h *Handler) allowedRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	return slices.Contains(h.opts.AllowedRedirects, u.Scheme+"://"+u.Host)
}

// redirectWith sends the browser to page with one more query parameter.
func redirectWith(w http.ResponseWriter, r *http.Request, page, key, value string) {
	u, err := url.Parse(page)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "invalid page to return to")
		return
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusFound)
}
