package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/oauth2"
)

// fakeProvider plays Google's or GitHub's token endpoint and API. It checks the PKCE verifier against the
// challenge from the auth URL, as the real ones do.
func fakeProvider(t *testing.T, challenge *string, api map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm() //nolint:errcheck
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != *challenge ||
			r.Form.Get("client_secret") != "secret" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"at-1","token_type":"bearer"}`)) //nolint:errcheck
	})
	for path, body := range api {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer at-1" || r.Header.Get("User-Agent") == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(body) //nolint:errcheck
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// authorize reads the challenge from the auth URL, like the provider's consent page would.
func authorize(t *testing.T, p Provider, verifier string) string {
	t.Helper()
	u, err := url.Parse(p.AuthURL("state-1", verifier))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") != "state-1" || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "http://gw/cb" {
		t.Fatalf("unexpected auth URL %s", u)
	}
	return q.Get("code_challenge")
}

func TestGoogle(t *testing.T) {
	var challenge string
	srv := fakeProvider(t, &challenge, map[string]any{
		"/userinfo": map[string]any{"sub": "g-123", "email": "ada@gmail.com", "email_verified": true, "name": "Ada L"},
	})
	g := NewGoogle("client", "secret", "http://gw/cb", Endpoints{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", APIURL: srv.URL + "/userinfo"})
	verifier := oauth2.GenerateVerifier()
	challenge = authorize(t, g, verifier)

	id, err := g.Identify(context.Background(), "good-code", verifier)
	if err != nil {
		t.Fatal(err)
	}
	if id != (Identity{Subject: "g-123", Email: "ada@gmail.com", EmailVerified: true, Username: "ada", Name: "Ada L"}) {
		t.Fatalf("unexpected identity %+v", id)
	}
	if _, err := g.Identify(context.Background(), "good-code", oauth2.GenerateVerifier()); err == nil {
		t.Fatal("a code with the wrong PKCE verifier must be refused")
	}
}

func TestGitHubUsesThePrimaryVerifiedEmail(t *testing.T) {
	var challenge string
	srv := fakeProvider(t, &challenge, map[string]any{
		"/user": map[string]any{"id": 42, "login": "octo-cat", "name": ""},
		"/user/emails": []map[string]any{
			{"email": "other@example.com", "primary": false, "verified": true},
			{"email": "octo@example.com", "primary": true, "verified": true},
		},
	})
	g := NewGitHub("client", "secret", "http://gw/cb", Endpoints{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", APIURL: srv.URL})
	verifier := oauth2.GenerateVerifier()
	challenge = authorize(t, g, verifier)

	id, err := g.Identify(context.Background(), "good-code", verifier)
	if err != nil {
		t.Fatal(err)
	}
	if id.Subject != "42" || id.Email != "octo@example.com" || !id.EmailVerified || id.Username != "octo-cat" {
		t.Fatalf("unexpected identity %+v", id)
	}
}

func TestGitHubUnverifiedPrimaryEmail(t *testing.T) {
	var challenge string
	srv := fakeProvider(t, &challenge, map[string]any{
		"/user":        map[string]any{"id": 7, "login": "x"},
		"/user/emails": []map[string]any{{"email": "x@example.com", "primary": true, "verified": false}},
	})
	g := NewGitHub("client", "secret", "http://gw/cb", Endpoints{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", APIURL: srv.URL})
	verifier := oauth2.GenerateVerifier()
	challenge = authorize(t, g, verifier)
	id, err := g.Identify(context.Background(), "good-code", verifier)
	if err != nil || id.EmailVerified {
		t.Fatalf("an unverified email must be reported as such: %+v %v", id, err)
	}
}
