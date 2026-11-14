package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// browser sends a request the way the frontend does: asking for the cookie transport and carrying the cookie.
func (ts *testServer) browser(t *testing.T, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token-Transport", "cookie")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	ts.h.ServeHTTP(rec, req)
	return rec
}

func refreshCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			return c
		}
	}
	t.Fatalf("no refresh_token cookie set: %v", rec.Header().Values("Set-Cookie"))
	return nil
}

func TestBrowserGetsTheRefreshTokenOnlyAsACookie(t *testing.T) {
	ts := newTestServer(t)
	ts.register(t, "gil@example.com", "gil", "correct horse")

	rec := ts.browser(t, "/auth/login", `{"email":"gil@example.com","password":"correct horse"}`, nil)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "refresh_token") {
		t.Fatal("a cookie client must not see the refresh token in the body")
	}
	c := refreshCookie(t, rec)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/auth" || c.MaxAge <= 0 || c.Value == "" {
		t.Fatalf("cookie attributes: %+v", c)
	}

	// Refresh with the cookie alone: a new access token, and the cookie rotates.
	rec = ts.browser(t, "/auth/refresh", `{}`, c)
	if rec.Code != 200 || decodeAs[tokenResponse](t, rec).AccessToken == "" {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	next := refreshCookie(t, rec)
	if next.Value == c.Value {
		t.Fatal("refresh must rotate the cookie's token")
	}

	// The old cookie again is a reuse: refused, the cookie cleared, and the session revoked.
	rec = ts.browser(t, "/auth/refresh", `{}`, c)
	expectError(t, rec, 401, "invalid_refresh_token")
	if refreshCookie(t, rec).MaxAge >= 0 {
		t.Fatal("a refused refresh must clear the cookie")
	}
	expectError(t, ts.browser(t, "/auth/refresh", `{}`, next), 401, "invalid_refresh_token")
}

func TestBrowserLogoutRevokesAndClearsTheCookie(t *testing.T) {
	ts := newTestServer(t)
	ts.register(t, "hal@example.com", "hal", "correct horse")
	c := refreshCookie(t, ts.browser(t, "/auth/login", `{"email":"hal@example.com","password":"correct horse"}`, nil))

	rec := ts.browser(t, "/auth/logout", `{}`, c)
	if rec.Code != 204 || refreshCookie(t, rec).MaxAge >= 0 {
		t.Fatalf("logout: %d, cookie %v", rec.Code, rec.Header().Values("Set-Cookie"))
	}
	expectError(t, ts.browser(t, "/auth/refresh", `{}`, c), 401, "invalid_refresh_token")
}

func TestCookieIsIgnoredWithoutTheTransportHeader(t *testing.T) {
	ts := newTestServer(t)
	tr := ts.register(t, "ida@example.com", "ida", "correct horse")
	if tr.RefreshToken == "" {
		t.Fatal("a client that doesn't ask for the cookie keeps the body token")
	}

	// A cookie alone, without the header, is not accepted: only our frontend sends both.
	req := httptest.NewRequest("POST", "/auth/refresh", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: tr.RefreshToken})
	rec := httptest.NewRecorder()
	ts.h.ServeHTTP(rec, req)
	expectError(t, rec, 401, "invalid_refresh_token")
}
