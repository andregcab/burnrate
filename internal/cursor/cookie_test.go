package cursor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// makeCookie builds a cookie whose JWT expires at the given time. The signature
// is nonsense on purpose: we never verify it, we only read a claim the server
// enforces anyway.
func makeCookie(exp time.Time, encoded bool) string {
	payload, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "sub": "user"})
	jwt := "eyJhbGciOiJIUzI1NiJ9." +
		base64.RawURLEncoding.EncodeToString(payload) + ".sig"

	sep := "::"
	if encoded {
		sep = "%3A%3A"
	}
	return "user_01ABC" + sep + jwt
}

func TestCookieExpiryReadsTheJWTClaim(t *testing.T) {
	want := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)

	for _, encoded := range []bool{false, true} {
		got, err := CookieExpiry(makeCookie(want, encoded))
		if err != nil {
			t.Fatalf("encoded=%v: CookieExpiry() = %v", encoded, err)
		}
		if !got.Equal(want) {
			t.Errorf("encoded=%v: expiry = %v, want %v", encoded, got, want)
		}
	}
}

func TestCookieExpiryRejectsUnreadableCookies(t *testing.T) {
	tests := []struct{ name, cookie string }{
		{"empty", ""},
		{"no jwt", "user_01ABC"},
		{"not base64", "user_01ABC::header.!!!not-base64!!!.sig"},
		{"no exp claim", "user_01ABC::h." +
			base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user"}`)) + ".s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CookieExpiry(tt.cookie); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestExpiresWithin(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		expiry   time.Duration
		wantSoon bool
	}{
		{"fresh cookie", 45 * 24 * time.Hour, false},
		{"just outside the window", 8 * 24 * time.Hour, false},
		{"inside the window", 3 * 24 * time.Hour, true},
		{"already expired", -time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			soon, left := ExpiresWithin(makeCookie(now.Add(tt.expiry), false),
				ExpiryWarnWithin, now)
			if soon != tt.wantSoon {
				t.Errorf("soon = %v, want %v (left %v)", soon, tt.wantSoon, left)
			}
		})
	}
}

// An unreadable expiry must not produce a permanent warning. Nagging forever
// about something we cannot measure is worse than staying quiet.
func TestUnreadableExpiryDoesNotWarn(t *testing.T) {
	soon, _ := ExpiresWithin("garbage-with-no-jwt", ExpiryWarnWithin, time.Now())
	if soon {
		t.Error("warned about a cookie whose expiry could not be read")
	}
}

// The real cookie observed during development had a 60-day life. This is a
// canary: if Cursor shortens sessions materially, the warning window may need
// revisiting.
func TestExampleCookieLifetimeIsAsObserved(t *testing.T) {
	const issued, expires = 1784038589, 1789222589
	days := (expires - issued) / 86400
	if days < 30 {
		t.Errorf("observed session life is %d days; the %v warning window may be too short",
			days, ExpiryWarnWithin)
	}
	fmt.Printf("observed session lifetime: %d days\n", days)
}

func TestErrNoExpiryIsMatchable(t *testing.T) {
	_, err := CookieExpiry("")
	if !errors.Is(err, ErrNoExpiry) {
		t.Errorf("got %v, want ErrNoExpiry", err)
	}
}
