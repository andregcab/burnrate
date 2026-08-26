package cursor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Session cookie inspection.
//
// The cookie is "<userId>::<jwt>" and the JWT carries its own expiry. Reading
// it means the tool can warn days ahead instead of simply breaking one morning
// — which matters because the cookie cannot be refreshed programmatically. The
// only remedy is to fetch a new one from the browser, and that is much less
// annoying when it is expected.

// ErrNoExpiry means the cookie carried no readable expiry claim.
var ErrNoExpiry = errors.New("cookie has no readable expiry")

// ExpiryWarnWithin is how far ahead the HUD starts nagging. A week is enough
// notice to deal with it without the warning becoming wallpaper.
const ExpiryWarnWithin = 7 * 24 * time.Hour

// CookieExpiry returns when the session cookie stops working.
//
// This reads the JWT's `exp` claim without verifying the signature, which is
// correct here: we are not authenticating anything, just reading a timestamp
// the server will enforce regardless of what we believe about it.
func CookieExpiry(cookie string) (time.Time, error) {
	jwt := cookieJWT(cookie)
	if jwt == "" {
		return time.Time{}, ErrNoExpiry
	}

	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return time.Time{}, ErrNoExpiry
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decoding cookie payload: %w", err)
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("reading cookie claims: %w", err)
	}
	if claims.Exp == 0 {
		return time.Time{}, ErrNoExpiry
	}
	return time.Unix(claims.Exp, 0), nil
}

// cookieJWT pulls the JWT half out of the cookie.
//
// The browser stores the value percent-encoded, so the "::" separator arrives
// as %3A%3A. Both forms are handled because the cookie may be pasted from
// DevTools (encoded) or from a script (decoded).
func cookieJWT(cookie string) string {
	decoded := cookie
	if unescaped, err := url.QueryUnescape(cookie); err == nil {
		decoded = unescaped
	}
	if _, jwt, found := strings.Cut(decoded, "::"); found {
		return jwt
	}
	// Some sources omit the user-id prefix entirely.
	if strings.Count(decoded, ".") == 2 {
		return decoded
	}
	return ""
}

// ExpiresWithin reports whether the cookie lapses inside the given window, and
// how long is left. A cookie with no readable expiry is reported as fine, since
// guessing would produce a permanent false warning.
func ExpiresWithin(cookie string, window time.Duration, now time.Time) (bool, time.Duration) {
	exp, err := CookieExpiry(cookie)
	if err != nil {
		return false, 0
	}
	left := exp.Sub(now)
	return left <= window, left
}
