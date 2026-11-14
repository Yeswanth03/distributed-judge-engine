package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/oauth"
)

// fakeProvider stands in for Google or GitHub: the authorization code names which identity "signed in".
type fakeProvider struct {
	name      string
	ids       map[string]oauth.Identity
	verifiers []string // the PKCE verifier each Identify call received
}

func newFakeProvider(name string) *fakeProvider {
	return &fakeProvider{name: name, ids: map[string]oauth.Identity{}}
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) AuthURL(state, verifier string) string {
	return "https://" + f.name + ".test/auth?state=" + url.QueryEscape(state)
}

func (f *fakeProvider) Identify(_ context.Context, code, verifier string) (oauth.Identity, error) {
	f.verifiers = append(f.verifiers, verifier)
	id, ok := f.ids[code]
	if !ok {
		return oauth.Identity{}, errors.New("bad code")
	}
	return id, nil
}

const page = "http://localhost:5500/"

// signIn runs a provider sign-in up to the redirect back to the page, returning that redirect's query.
func (ts *testServer) signIn(t *testing.T, provider string, id oauth.Identity) url.Values {
	t.Helper()
	ts.providers[provider].ids["code-"+id.Subject] = id
	start := ts.do(t, "GET", "/auth/oauth/"+provider+"/start?redirect_uri="+url.QueryEscape(page), nil, "")
	if start.Code != http.StatusFound {
		t.Fatalf("start: %d %s", start.Code, start.Body)
	}
	loc, _ := url.Parse(start.Header().Get("Location"))
	state := loc.Query().Get("state")
	cb := ts.do(t, "GET", "/auth/oauth/"+provider+"/callback?code=code-"+url.QueryEscape(id.Subject)+"&state="+url.QueryEscape(state), nil, "")
	if cb.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", cb.Code, cb.Body)
	}
	back, _ := url.Parse(cb.Header().Get("Location"))
	if !strings.HasPrefix(back.String(), page) {
		t.Fatalf("must return to the page, went to %s", back)
	}
	return back.Query()
}

func (ts *testServer) exchange(t *testing.T, code string) tokenResponse {
	t.Helper()
	rec := ts.do(t, "POST", "/auth/oauth/exchange", nil, `{"login_code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange: %d %s", rec.Code, rec.Body)
	}
	return decodeAs[tokenResponse](t, rec)
}

func TestOAuthSignInCreatesAnAccount(t *testing.T) {
	ts := newTestServer(t)
	q := ts.signIn(t, "google", oauth.Identity{Subject: "g-1", Email: "ada@gmail.com", EmailVerified: true, Username: "ada.l", Name: "Ada Lovelace"})
	if q.Get("login_error") != "" || q.Get("login_code") == "" || q.Has("access_token") {
		t.Fatalf("expected only a login code in the URL, got %v", q)
	}
	tr := ts.exchange(t, q.Get("login_code"))
	if tr.User.Email != "ada@gmail.com" || tr.User.Username != "ada_l" || tr.User.DisplayName != "Ada Lovelace" ||
		!tr.User.EmailVerified || tr.User.Role != "user" {
		t.Fatalf("unexpected account %+v", tr.User)
	}
	if ts.claimsOf(t, tr.AccessToken).Subject != tr.User.UserID.String() {
		t.Fatal("the access token must be for the new account")
	}
	if len(ts.providers["google"].verifiers) != 1 || ts.providers["google"].verifiers[0] == "" {
		t.Fatal("the PKCE verifier must be passed to the code exchange")
	}

	// Signing in again with the same Google account is the same user, not a new one.
	again := ts.exchange(t, ts.signIn(t, "google", oauth.Identity{Subject: "g-1", Email: "ada@gmail.com", EmailVerified: true, Username: "ada.l"}).Get("login_code"))
	if again.User.UserID != tr.User.UserID {
		t.Fatal("the same provider account must reach the same user")
	}
	// The account has no password, so password login can't get in.
	expectError(t, ts.do(t, "POST", "/auth/login", nil, `{"email":"ada@gmail.com","password":"anything at all"}`), 401, "invalid_credentials")
}

func TestOAuthCodesAreSingleUse(t *testing.T) {
	ts := newTestServer(t)
	q := ts.signIn(t, "github", oauth.Identity{Subject: "7", Email: "bo@example.com", EmailVerified: true, Username: "bo"})
	ts.exchange(t, q.Get("login_code"))
	expectError(t, ts.do(t, "POST", "/auth/oauth/exchange", nil, `{"login_code":"`+q.Get("login_code")+`"}`), 401, "invalid_login_code")

	// Replaying the provider's callback (same state) is refused too.
	start := ts.do(t, "GET", "/auth/oauth/github/start?redirect_uri="+url.QueryEscape(page), nil, "")
	loc, _ := url.Parse(start.Header().Get("Location"))
	cb := "/auth/oauth/github/callback?code=code-7&state=" + url.QueryEscape(loc.Query().Get("state"))
	if rec := ts.do(t, "GET", cb, nil, ""); rec.Code != http.StatusFound {
		t.Fatalf("first callback: %d", rec.Code)
	}
	expectError(t, ts.do(t, "GET", cb, nil, ""), 400, "invalid_state")
	expectError(t, ts.do(t, "GET", "/auth/oauth/github/callback?code=code-7&state=made-up", nil, ""), 400, "invalid_state")
}

func TestOAuthNeverTakesOverAPasswordAccount(t *testing.T) {
	ts := newTestServer(t)
	ts.register(t, "victim@example.com", "victim", "correct horse")
	// Someone signs up at a provider with the victim's address. Their email is verified there; the password
	// account's isn't, so linking would hand them the account.
	q := ts.signIn(t, "github", oauth.Identity{Subject: "99", Email: "victim@example.com", EmailVerified: true, Username: "attacker"})
	if q.Get("login_error") != "email_in_use" || q.Has("login_code") {
		t.Fatalf("expected email_in_use, got %v", q)
	}
}

func TestOAuthLinksProvidersThatVerifiedTheSameEmail(t *testing.T) {
	ts := newTestServer(t)
	first := ts.exchange(t, ts.signIn(t, "google", oauth.Identity{Subject: "g-5", Email: "cy@example.com", EmailVerified: true, Username: "cy"}).Get("login_code"))
	second := ts.exchange(t, ts.signIn(t, "github", oauth.Identity{Subject: "55", Email: "CY@example.com", EmailVerified: true, Username: "cy-gh"}).Get("login_code"))
	if second.User.UserID != first.User.UserID {
		t.Fatal("both providers verified this email, so they're the same person")
	}
}

func TestOAuthRefusals(t *testing.T) {
	ts := newTestServer(t)
	q := ts.signIn(t, "github", oauth.Identity{Subject: "1", Email: "u@example.com", EmailVerified: false, Username: "u"})
	if q.Get("login_error") != "email_not_verified" {
		t.Fatalf("an unverified email must be refused, got %v", q)
	}

	// The user clicked "cancel" at the provider.
	start := ts.do(t, "GET", "/auth/oauth/google/start?redirect_uri="+url.QueryEscape(page), nil, "")
	loc, _ := url.Parse(start.Header().Get("Location"))
	cb := ts.do(t, "GET", "/auth/oauth/google/callback?error=access_denied&state="+url.QueryEscape(loc.Query().Get("state")), nil, "")
	back, _ := url.Parse(cb.Header().Get("Location"))
	if back.Query().Get("login_error") != "cancelled" {
		t.Fatalf("expected cancelled, got %s", back)
	}

	for _, target := range []string{"https://evil.example/", "http://localhost:5500.evil.example/", "javascript:alert(1)", "/relative", ""} {
		expectError(t, ts.do(t, "GET", "/auth/oauth/google/start?redirect_uri="+url.QueryEscape(target), nil, ""), 400, "bad_request")
	}
	expectError(t, ts.do(t, "GET", "/auth/oauth/myspace/start?redirect_uri="+url.QueryEscape(page), nil, ""), 404, "not_found")
}

func TestOAuthUsernameClashGetsASuffix(t *testing.T) {
	ts := newTestServer(t)
	ts.register(t, "dee@example.com", "dee", "correct horse")
	tr := ts.exchange(t, ts.signIn(t, "github", oauth.Identity{Subject: "3", Email: "other@example.com", EmailVerified: true, Username: "dee"}).Get("login_code"))
	if !strings.HasPrefix(tr.User.Username, "dee_") || len(tr.User.Username) != len("dee_")+4 {
		t.Fatalf("expected dee_NNNN, got %q", tr.User.Username)
	}
}

func TestOAuthProvidersList(t *testing.T) {
	ts := newTestServer(t)
	type list struct{ Providers []string }
	if got := decodeAs[list](t, ts.do(t, "GET", "/auth/oauth/providers", nil, "")).Providers; strings.Join(got, ",") != "github,google" {
		t.Fatalf("got %v", got)
	}
}

func TestUsernameFrom(t *testing.T) {
	for in, want := range map[string]string{"ada.l": "ada_l", "x": "x_user", "--a--": "a_user", "Grace_Hopper": "Grace_Hopper",
		strings.Repeat("z", 40): strings.Repeat("z", 30)} {
		if got := usernameFrom(in); got != want {
			t.Errorf("usernameFrom(%q) = %q, want %q", in, got, want)
		}
	}
}
