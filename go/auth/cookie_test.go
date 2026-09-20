package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testCookieConfig() CookieConfig {
	return CookieConfig{
		Name:     "mc_refresh",
		Path:     "/metacensus/api/v1/refresh",
		MaxAge:   24 * time.Hour,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

// handlerWriting returns a handler that writes status and body verbatim, as the
// generated handler stack would after respond().
func handlerWriting(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// TestAdapterMovesTokenToCookie: a Session body's refresh_token is lifted into
// an HttpOnly cookie and blanked from the JSON the browser receives.
func TestAdapterMovesTokenToCookie(t *testing.T) {
	h := CookieAdapter(testCookieConfig())(handlerWriting(
		http.StatusOK, `{"token":"access","expiresIn":900,"refreshToken":"secret"}`))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/metacensus/api/v1/login", nil))

	res := rec.Result()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != "mc_refresh" || c.Value != "secret" {
		t.Errorf("cookie = %s=%s, want mc_refresh=secret", c.Name, c.Value)
	}
	if !c.HttpOnly || !c.Secure {
		t.Errorf("cookie flags: HttpOnly=%v Secure=%v, want both true", c.HttpOnly, c.Secure)
	}
	if c.SameSite != http.SameSiteStrictMode || c.Path != "/metacensus/api/v1/refresh" {
		t.Errorf("cookie scope: SameSite=%v Path=%q", c.SameSite, c.Path)
	}
	if c.MaxAge != int(24*time.Hour/time.Second) {
		t.Errorf("cookie MaxAge = %d, want %d", c.MaxAge, int(24*time.Hour/time.Second))
	}

	// The body the browser sees no longer carries the token, but keeps the rest.
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, present := body["refreshToken"]; present {
		t.Error("refreshToken still present in the response body")
	}
	if body["token"] != "access" {
		t.Errorf("token field = %v, want access", body["token"])
	}
}

// TestAdapterReadsCookieInbound: a request's cookie reaches the handler through
// RefreshCookieFrom.
func TestAdapterReadsCookieInbound(t *testing.T) {
	var seen string
	var ok bool
	h := CookieAdapter(testCookieConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, ok = RefreshCookieFrom(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	req := httptest.NewRequest("POST", "/metacensus/api/v1/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "mc_refresh", Value: "from-cookie"})
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !ok || seen != "from-cookie" {
		t.Fatalf("handler saw refresh cookie %q (ok=%v), want from-cookie", seen, ok)
	}
}

// TestAdapterClearsCookieOnLogout: a 2xx with no refresh_token, on a request
// that carried the cookie, clears it.
func TestAdapterClearsCookieOnLogout(t *testing.T) {
	h := CookieAdapter(testCookieConfig())(handlerWriting(http.StatusOK, `{}`))

	req := httptest.NewRequest("POST", "/metacensus/api/v1/logout", nil)
	req.AddCookie(&http.Cookie{Name: "mc_refresh", Value: "to-be-cleared"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if c := cookies[0]; c.MaxAge >= 0 || c.Value != "" {
		t.Errorf("cookie not cleared: value=%q MaxAge=%d", c.Value, c.MaxAge)
	}
}

// TestAdapterLeavesErrorsAlone: a non-2xx passes through with its body intact
// and sets no cookie, even when the request carried one.
func TestAdapterLeavesErrorsAlone(t *testing.T) {
	body := `{"error":"authentication required","code":"unauthenticated"}`
	h := CookieAdapter(testCookieConfig())(handlerWriting(http.StatusUnauthorized, body))

	req := httptest.NewRequest("POST", "/metacensus/api/v1/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "mc_refresh", Value: "stale"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("an error response should set no cookie")
	}
	if strings.TrimSpace(rec.Body.String()) != body {
		t.Errorf("error body altered: %s", rec.Body.String())
	}
}

// TestAdapterNoTokenNoCookie: a login-shaped response whose refresh_token is
// empty, on a request with no inbound cookie, sets no cookie at all.
func TestAdapterNoTokenNoCookie(t *testing.T) {
	h := CookieAdapter(testCookieConfig())(handlerWriting(
		http.StatusOK, `{"token":"access","expiresIn":900,"refreshToken":""}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/metacensus/api/v1/login", nil))
	if len(rec.Result().Cookies()) != 0 {
		t.Error("an empty refresh token should set no cookie")
	}
}
