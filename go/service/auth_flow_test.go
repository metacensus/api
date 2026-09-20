package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contract "github.com/metacensus/api/go/contract"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/server/routes"
	"google.golang.org/protobuf/proto"
)

// send drives one request against the mux, attaching a bearer token and a
// refresh cookie when given, and marshalling body with the contract encoder.
func send(t *testing.T, mux http.Handler, method, path, token string, cookie *http.Cookie, body proto.Message) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := contract.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		r = httptest.NewRequest(method, routes.Prefix+path, strings.NewReader(string(raw)))
	} else {
		r = httptest.NewRequest(method, routes.Prefix+path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

// refreshCookie returns the mc_refresh cookie a response set, or nil.
func refreshCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "mc_refresh" {
			return c
		}
	}
	return nil
}

// TestAuthTokenLifecycle walks the whole session posture: sign-up mints an
// access token with an expiry and an HttpOnly refresh cookie; the cookie
// refreshes into a fresh access token, rotating; and logout revokes the
// lineage so neither the cookie nor the last access token works after.
func TestAuthTokenLifecycle(t *testing.T) {
	_, mux := newTestServer(t)
	c := newClient(t, mux)

	// Sign up.
	user := &v1.User{Name: "Ada", Email: "ada@example.com"}
	suInterp, suSig := c.sign(user)
	signup := c.do("POST", "/signup", &v1.SignUpRequest{
		Content: user, Password: "pw", Interpretation: suInterp, PublicKey: c.publicKey(), UserSignature: suSig,
	})
	var session v1.Session
	decode(t, signup, &session)

	// The access token carries an explicit expiry (default 15 minutes).
	if session.GetExpiresIn() != 900 {
		t.Errorf("expires_in = %d, want 900", session.GetExpiresIn())
	}
	// The refresh token never reaches the JSON body — it is in the cookie.
	if session.GetRefreshToken() != "" {
		t.Error("refresh token leaked into the response body")
	}
	cookie := refreshCookie(signup)
	if cookie == nil || cookie.Value == "" {
		t.Fatal("sign-up set no refresh cookie")
	}
	if !cookie.HttpOnly || cookie.Path != routes.Prefix+"/refresh" {
		t.Errorf("refresh cookie: HttpOnly=%v Path=%q", cookie.HttpOnly, cookie.Path)
	}

	// The access token authenticates a protected route.
	if self := send(t, mux, "GET", "/self", session.GetToken(), nil, nil); self.Code != http.StatusOK {
		t.Fatalf("get self with access token: %d, body %s", self.Code, self.Body.String())
	}

	// Refresh with the cookie (no bearer token, empty body): a fresh session,
	// rotated cookie, access token still absent from the body.
	refreshed := send(t, mux, "POST", "/refresh", "", cookie, &v1.RefreshRequest{})
	var next v1.Session
	decode(t, refreshed, &next)
	if next.GetToken() == "" || next.GetToken() == session.GetToken() {
		t.Error("refresh did not mint a new access token")
	}
	if next.GetRefreshToken() != "" {
		t.Error("refresh leaked the refresh token into the body")
	}
	rotated := refreshCookie(refreshed)
	if rotated == nil || rotated.Value == cookie.Value {
		t.Fatal("refresh did not rotate the cookie")
	}

	// The rotated access token works; the consumed refresh cookie is now dead.
	if got := send(t, mux, "GET", "/self", next.GetToken(), nil, nil); got.Code != http.StatusOK {
		t.Errorf("get self with rotated token: %d", got.Code)
	}
	if got := send(t, mux, "POST", "/refresh", "", cookie, &v1.RefreshRequest{}); got.Code != http.StatusUnauthorized {
		t.Errorf("replaying the consumed refresh cookie: %d, want 401", got.Code)
	}

	// Log out with the current cookie: the cookie is cleared, and the session
	// is revoked — the last access token and refresh cookie both stop working.
	logout := send(t, mux, "POST", "/logout", "", rotated, &v1.LogoutRequest{})
	if logout.Code != http.StatusOK {
		t.Fatalf("logout: %d, body %s", logout.Code, logout.Body.String())
	}
	if cleared := refreshCookie(logout); cleared == nil || cleared.MaxAge >= 0 {
		t.Error("logout did not clear the refresh cookie")
	}
	if got := send(t, mux, "GET", "/self", next.GetToken(), nil, nil); got.Code != http.StatusUnauthorized {
		t.Errorf("access token after logout: %d, want 401", got.Code)
	}
	if got := send(t, mux, "POST", "/refresh", "", rotated, &v1.RefreshRequest{}); got.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout: %d, want 401", got.Code)
	}
}

// TestRefreshWithoutCredential: a refresh with neither cookie nor body token is
// a 401, not a 500.
func TestRefreshWithoutCredential(t *testing.T) {
	_, mux := newTestServer(t)
	if got := send(t, mux, "POST", "/refresh", "", nil, &v1.RefreshRequest{}); got.Code != http.StatusUnauthorized {
		t.Fatalf("refresh with no credential: %d, want 401", got.Code)
	}
}

// TestRefreshViaBodyToken is the proxy posture: no cookie, refresh token in the
// body field. It refreshes just the same, proving the contract is
// cookie-agnostic and the adapter is the only thing that knows about cookies.
func TestRefreshViaBodyToken(t *testing.T) {
	_, mux := newTestServer(t)
	c := newClient(t, mux)

	user := &v1.User{Name: "Ada", Email: "ada@example.com"}
	suInterp, suSig := c.sign(user)
	signup := c.do("POST", "/signup", &v1.SignUpRequest{
		Content: user, Password: "pw", Interpretation: suInterp, PublicKey: c.publicKey(), UserSignature: suSig,
	})
	cookie := refreshCookie(signup)
	if cookie == nil {
		t.Fatal("sign-up set no refresh cookie")
	}

	// Send the token in the body, no cookie — as a proxy relaying it would.
	got := send(t, mux, "POST", "/refresh", "", nil, &v1.RefreshRequest{RefreshToken: cookie.Value})
	var next v1.Session
	decode(t, got, &next)
	if next.GetToken() == "" {
		t.Error("body-token refresh returned no access token")
	}
}
