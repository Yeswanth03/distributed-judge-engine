package handler

import (
	"net/http"
	"strings"
)

// The refresh token for browsers travels in an HttpOnly cookie, so no script on the page can read it
// (docs/contracts.md section 7). Other clients (scripts, tests, curl) keep getting it in the JSON body.
const (
	refreshCookieName = "refresh_token"
	// Scoped to the auth routes: the browser never sends it to any other service behind the gateway. A proxy that
	// serves these routes under another prefix rewrites the path (the frontend's /api/auth).
	refreshCookiePath = "/auth"
	// A browser client asks for the cookie with this header; its value is "cookie".
	tokenTransportHeader = "X-Token-Transport"
)

func wantsCookie(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get(tokenTransportHeader), "cookie")
}

// refreshTokenOf is the refresh token a request presents: the body's, else (for cookie clients) the cookie's.
func refreshTokenOf(r *http.Request, req refreshRequest) string {
	if req.RefreshToken != "" {
		return req.RefreshToken
	}
	if c, err := r.Cookie(refreshCookieName); err == nil && wantsCookie(r) {
		return c.Value
	}
	return ""
}

// writeTokens answers with tokens. For a cookie client the refresh token goes into the cookie and out of the body.
func (h *Handler) writeTokens(w http.ResponseWriter, r *http.Request, status int, resp tokenResponse) {
	if wantsCookie(r) {
		http.SetCookie(w, &http.Cookie{
			Name:     refreshCookieName,
			Value:    resp.RefreshToken,
			Path:     refreshCookiePath,
			MaxAge:   resp.RefreshExpiresIn,
			HttpOnly: true,
			Secure:   h.opts.RefreshCookieSecure,
			// Never sent on a request started by another site, which rules out CSRF on refresh and logout.
			SameSite: http.SameSiteStrictMode,
		})
		resp.RefreshToken = ""
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, resp)
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.opts.RefreshCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}
