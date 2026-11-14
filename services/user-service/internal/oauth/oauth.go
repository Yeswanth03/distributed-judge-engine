// Package oauth signs users in with Google or GitHub: the authorization code flow with PKCE, run entirely on
// the server, so the client secrets never reach a browser.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
)

// Identity is who the provider says signed in.
type Identity struct {
	Subject       string // the provider's stable user ID; emails and usernames can change
	Email         string
	EmailVerified bool
	Username      string // a suggestion for our username (GitHub login, or the email's local part)
	Name          string
}

// Provider is one sign-in provider.
type Provider interface {
	Name() string
	// AuthURL is where to send the browser: the provider's consent page.
	AuthURL(state, verifier string) string
	// Identify exchanges the authorization code (with its PKCE verifier) and returns who signed in.
	Identify(ctx context.Context, code, verifier string) (Identity, error)
}

// Endpoints are a provider's URLs. Tests point them at a fake provider.
type Endpoints struct {
	AuthURL  string
	TokenURL string
	APIURL   string // Google: the userinfo endpoint; GitHub: the API root
}

var (
	GoogleEndpoints = Endpoints{
		AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",
		APIURL:   "https://openidconnect.googleapis.com/v1/userinfo",
	}
	GitHubEndpoints = Endpoints{
		AuthURL:  "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token",
		APIURL:   "https://api.github.com",
	}
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

type base struct {
	name string
	cfg  oauth2.Config
	api  string
}

func newBase(name, clientID, secret, redirectURL string, ep Endpoints, scopes []string) base {
	return base{name: name, api: ep.APIURL, cfg: oauth2.Config{
		ClientID: clientID, ClientSecret: secret, RedirectURL: redirectURL, Scopes: scopes,
		Endpoint: oauth2.Endpoint{AuthURL: ep.AuthURL, TokenURL: ep.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}}
}

func (b base) Name() string { return b.name }

func (b base) exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	tok, err := b.cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("%s: exchange code: %w", b.name, err)
	}
	return tok, nil
}

// getJSON calls the provider's API with the access token and decodes the answer.
func getJSON(ctx context.Context, url, accessToken string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "judge-engine-user-service") // GitHub's API requires one
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: %s: %s", url, resp.Status, body)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(into)
}

// Google signs in with OpenID Connect scopes and reads the profile from the userinfo endpoint, fetched over TLS
// with the access token Google just issued to us.
type Google struct{ base }

func NewGoogle(clientID, secret, redirectURL string, ep Endpoints) *Google {
	return &Google{newBase("google", clientID, secret, redirectURL, ep, []string{"openid", "email", "profile"})}
}

func (g *Google) AuthURL(state, verifier string) string {
	return g.cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"))
}

func (g *Google) Identify(ctx context.Context, code, verifier string) (Identity, error) {
	tok, err := g.exchange(ctx, code, verifier)
	if err != nil {
		return Identity{}, err
	}
	var info struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := getJSON(ctx, g.api, tok.AccessToken, &info); err != nil {
		return Identity{}, fmt.Errorf("google: userinfo: %w", err)
	}
	if info.Sub == "" {
		return Identity{}, errors.New("google: userinfo without sub")
	}
	return Identity{Subject: info.Sub, Email: info.Email, EmailVerified: info.EmailVerified,
		Username: localPart(info.Email), Name: info.Name}, nil
}

// GitHub reads the profile and the primary verified email from the REST API.
type GitHub struct{ base }

func NewGitHub(clientID, secret, redirectURL string, ep Endpoints) *GitHub {
	return &GitHub{newBase("github", clientID, secret, redirectURL, ep, []string{"read:user", "user:email"})}
}

func (g *GitHub) AuthURL(state, verifier string) string {
	return g.cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

func (g *GitHub) Identify(ctx context.Context, code, verifier string) (Identity, error) {
	tok, err := g.exchange(ctx, code, verifier)
	if err != nil {
		return Identity{}, err
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := getJSON(ctx, g.api+"/user", tok.AccessToken, &user); err != nil {
		return Identity{}, fmt.Errorf("github: user: %w", err)
	}
	if user.ID == 0 {
		return Identity{}, errors.New("github: user without id")
	}
	// The profile's own email can be hidden or unverified; /user/emails says which one is primary and verified.
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, g.api+"/user/emails", tok.AccessToken, &emails); err != nil {
		return Identity{}, fmt.Errorf("github: emails: %w", err)
	}
	id := Identity{Subject: strconv.FormatInt(user.ID, 10), Username: user.Login, Name: user.Name}
	for _, e := range emails {
		if e.Primary {
			id.Email, id.EmailVerified = e.Email, e.Verified
		}
	}
	return id, nil
}

func localPart(email string) string {
	for i := range len(email) {
		if email[i] == '@' {
			return email[:i]
		}
	}
	return email
}
