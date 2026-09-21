package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// CookieConfig configures the HttpOnly refresh cookie the CookieAdapter keeps.
// The zero value is not usable; the service fills it (see service.Config).
type CookieConfig struct {
	Name string
	// Path scopes which requests the browser attaches the cookie to. Both routes
	// that read it, refresh and logout, must fall under it (an RFC 6265 path only
	// matches requests at or below it), so set it to the shared API prefix rather
	// than one route's full path.
	Path string
	// Match it to the refresh-token TTL.
	MaxAge time.Duration
	// Secure gates the cookie to https. True in production; a plain-http test
	// sets it false so the token round-trips.
	Secure bool
	// http.SameSiteStrictMode suits a same-origin refresh.
	SameSite http.SameSite
}

// CookieAdapter is the session-cookie adapter: the one piece that knows the
// refresh token can live in an HttpOnly cookie rather than the JSON body. It
// bridges the cookie-agnostic contract to a browser both ways:
//
//   - inbound, it reads the cookie off the request and carries the token on the
//     context, where the Refresh and Logout handlers read it (RefreshCookieFrom);
//   - outbound, on a 2xx it lifts a `refresh_token` out of the response body
//     into a Set-Cookie and blanks the field, so a browser never holds the token
//     in JavaScript. A successful response that carries no token, on a request
//     that did carry the cookie, is a logout: the cookie is cleared.
//
// Handlers and the Sessions port never mention cookies, so this whole adapter
// lifts out to a reverse proxy unchanged: drop it, and the API speaks
// `refresh_token` in the body over the trusted hop instead.
func CookieAdapter(cfg CookieConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hadCookie := false
			if c, err := r.Cookie(cfg.Name); err == nil && c.Value != "" {
				hadCookie = true
				r = r.WithContext(context.WithValue(r.Context(), refreshCookieKey, c.Value))
			}

			cap := &captureWriter{header: http.Header{}}
			next.ServeHTTP(cap, r)

			status := cap.status
			if status == 0 {
				status = http.StatusOK
			}
			body := cap.buf.Bytes()
			if status >= 200 && status < 300 {
				if token, stripped, ok := stripRefreshToken(body); ok && token != "" {
					http.SetCookie(w, cfg.cookie(token, int(cfg.MaxAge/time.Second)))
					body = stripped
				} else if hadCookie {
					http.SetCookie(w, cfg.cookie("", -1))
				}
			}

			copyHeader(w.Header(), cap.header)
			// The body may have lost its refresh_token; let net/http recompute
			// the length rather than trust the captured one.
			w.Header().Del("Content-Length")
			w.WriteHeader(status)
			_, _ = w.Write(body)
		})
	}
}

// cookie builds the refresh cookie with the given value and Max-Age; a negative
// maxAge and empty value clears it.
func (cfg CookieConfig) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     cfg.Name,
		Value:    value,
		Path:     cfg.Path,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: cfg.SameSite,
		MaxAge:   maxAge,
	}
}

// stripRefreshToken pulls a refresh_token out of a JSON object body and returns
// it with a copy of the body that no longer carries it. ok is false when the
// body is not a JSON object or has no such field, in which case the body is
// returned untouched. The field is always the camelCase `refreshToken`: the
// adapter only ever rewrites this server's own responses, which the contract
// marshals in camelCase.
func stripRefreshToken(body []byte) (token string, stripped []byte, ok bool) {
	if len(bytes.TrimSpace(body)) == 0 {
		return "", body, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return "", body, false
	}
	raw, present := m["refreshToken"]
	if !present {
		return "", body, false
	}
	if err := json.Unmarshal(raw, &token); err != nil {
		return "", body, false
	}
	delete(m, "refreshToken")
	out, err := json.Marshal(m)
	if err != nil {
		return "", body, false
	}
	return token, out, true
}

// captureWriter buffers a handler's response so the adapter can set a cookie
// (a header) and rewrite the body before either is committed — the generated
// handler writes the whole response, so there is no earlier seam to use.
type captureWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (c *captureWriter) Header() http.Header { return c.header }

func (c *captureWriter) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
