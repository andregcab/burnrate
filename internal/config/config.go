// Package config loads burnrate's settings and locates the session cookie.
//
// Settings live in ~/.burnrate/config.toml. The cookie deliberately does not:
// it goes in the macOS Keychain, so it never sits in a dotfile, a shell history
// entry, or a process argument list.
package config

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/zalando/go-keyring"
)

const (
	// legacyCookieService is the pre-rename Keychain name. Reads fall back to
	// it so an existing install keeps working across the rename.
	legacyCookieService = "cursor-arcade-cookie"

	// KeychainCookieService holds the browser session cookie. It is separate
	// from the API key item because they are different credentials with very
	// different blast radii: the cookie is a full logged-in session.
	KeychainCookieService = "burnrate-cookie"

	// EnvCookie is the fallback source for the session cookie.
	EnvCookie = "CURSOR_SESSION_COOKIE"

	defaultRefresh = 5 * time.Minute

	// minRefresh keeps us clear of the tightest endpoint rate limit
	// (/teams/spend at 20 requests per minute).
	minRefresh = 30 * time.Second
)

// ErrNoCookie means we found no session cookie in either the Keychain or the
// environment.
var ErrNoCookie = errors.New("no session cookie found")

// Config is the on-disk settings file.
type Config struct {
	// MonthlyBudgetDollars overrides the spend limit the API reports, and is
	// the only source of a limit when the team sets no per-user cap. Nil means
	// "trust the API". This is the HP bar's denominator.
	MonthlyBudgetDollars *float64 `toml:"monthly_budget_dollars,omitempty"`

	// TeamID is required by the usage-events endpoint. `init` discovers it from
	// /api/auth/stripe so nobody has to find it by hand.
	TeamID int `toml:"team_id"`

	// RefreshSeconds is how often the TUI re-polls.
	RefreshSeconds int `toml:"refresh_seconds"`

	// Buddy is the companion shown by default, by name. Empty means the first
	// in the catalog. Written by pressing `s` in the TUI rather than by hand.
	Buddy string `toml:"buddy,omitempty"`

	// Machine is the default machine, by slug. Empty means the first.
	Machine string `toml:"machine,omitempty"`

	// MachineOn records whether the machine section was showing when the look
	// was saved, so `s` captures the whole arrangement rather than half of it.
	MachineOn bool `toml:"machine_on,omitempty"`
}

// Refresh returns the poll interval, clamped to something the rate limits allow.
func (c Config) Refresh() time.Duration {
	if c.RefreshSeconds <= 0 {
		return defaultRefresh
	}
	d := time.Duration(c.RefreshSeconds) * time.Second
	if d < minRefresh {
		return minRefresh
	}
	return d
}

// Dir is ~/.burnrate, where config and cache live.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".burnrate"), nil
}

// Path is the full path to config.toml.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load reads config.toml and applies environment overrides. A missing file is
// not an error: it yields a zero Config, which Validate will then reject with a
// message pointing at `burnrate init`.
func Load() (Config, error) {
	var c Config

	path, err := Path()
	if err != nil {
		return c, err
	}

	if _, err := toml.DecodeFile(path, &c); err != nil && !os.IsNotExist(err) {
		return c, fmt.Errorf("reading %s: %w", path, err)
	}

	// Environment wins over the file, so a one-off run can override without
	// editing anything.
	if v := os.Getenv("CURSOR_MONTHLY_BUDGET"); v != "" {
		d, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return c, fmt.Errorf("CURSOR_MONTHLY_BUDGET=%q is not a number: %w", v, err)
		}
		c.MonthlyBudgetDollars = &d
	}

	return c, nil
}

// SetLook stores the default companion, machine, and whether the machine is
// showing — the whole arrangement, saved as one.
//
// It re-reads before writing so saving a look cannot clobber the email, budget
// override, or refresh interval already in the file.
func SetLook(buddy, machine string, machineOn bool) error {
	c, err := Load()
	if err != nil {
		return err
	}
	c.Buddy = buddy
	c.Machine = machine
	c.MachineOn = machineOn
	return c.Save()
}

// Save writes config.toml with 0600 permissions, creating the directory if needed.
func (c Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	path := filepath.Join(dir, "config.toml")

	// Write to a temp file and rename, so an interrupted save cannot leave a
	// truncated config behind.
	tmp, err := os.CreateTemp(dir, "config-*.toml")
	if err != nil {
		return fmt.Errorf("creating temp config: %w", err)
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("securing temp config: %w", err)
	}
	if err := toml.NewEncoder(tmp).Encode(c); err != nil {
		tmp.Close()
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("saving %s: %w", path, err)
	}
	return nil
}

// keychainAccount is the Keychain item's account name. It matches what the M0
// probe uses (`security add-generic-password -a "$USER" -s burnrate`).
func keychainAccount() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	return "default"
}

// SessionCookie returns the WorkosCursorSessionToken value, preferring the
// Keychain and falling back to the environment.
//
// This is a full browser session, not a scoped token — anything you can do on
// the Cursor dashboard, it can do. It never belongs in config.toml.
func SessionCookie() (string, error) {
	for _, svc := range []string{KeychainCookieService, legacyCookieService} {
		if v, err := keyring.Get(svc, keychainAccount()); err == nil && v != "" {
			return v, nil
		}
	}
	if v := os.Getenv(EnvCookie); v != "" {
		return v, nil
	}
	return "", fmt.Errorf(
		"%w: run `burnrate init` to store your session cookie", ErrNoCookie)
}

// keyringGet reports whether a real cookie is stored on this machine. Tests
// that exercise the fallback paths skip when one is, rather than asserting
// against the developer's own Keychain.
func keyringGet() (string, error) {
	return keyring.Get(KeychainCookieService, keychainAccount())
}

// SetSessionCookie stores the session cookie in the Keychain.
func SetSessionCookie(cookie string) error {
	if cookie == "" {
		return errors.New("refusing to store an empty session cookie")
	}
	if err := keyring.Set(KeychainCookieService, keychainAccount(), cookie); err != nil {
		return fmt.Errorf("storing cookie in Keychain: %w", err)
	}
	return nil
}
