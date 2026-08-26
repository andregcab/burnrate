package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// keyringGet reports whether this machine already has a real key stored. Tests
// that exercise the fallback paths skip when it does, rather than asserting
// against — or worse, clobbering — the developer's own Keychain.
func keyringGet() (string, error) {
	return keyring.Get(KeychainService, keychainAccount())
}

// withHome points HOME at a temp dir so tests never touch the real
// ~/.burnrate.
func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func TestRefreshClamping(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{"zero falls back to default", 0, defaultRefresh},
		{"negative falls back to default", -10, defaultRefresh},
		{"below the floor is clamped", 5, minRefresh},
		{"at the floor is kept", 30, 30 * time.Second},
		{"normal value is kept", 300, 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Config{RefreshSeconds: tt.seconds}.Refresh()
			if got != tt.want {
				t.Errorf("Refresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	withHome(t)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() on a missing file returned %v, want nil", err)
	}
	if c.Email != "" {
		t.Errorf("Email = %q, want empty", c.Email)
	}
	// It should still be rejected by Validate, with actionable guidance.
	if err := c.Validate(); !errors.Is(err, ErrNoEmail) {
		t.Errorf("Validate() = %v, want ErrNoEmail", err)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	home := withHome(t)

	budget := 125.0
	want := Config{
		Email:                "someone@example.com",
		MonthlyBudgetDollars: &budget,
		RefreshSeconds:       300,
	}
	if err := want.Save(); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.Email != want.Email {
		t.Errorf("Email = %q, want %q", got.Email, want.Email)
	}
	if got.RefreshSeconds != want.RefreshSeconds {
		t.Errorf("RefreshSeconds = %d, want %d", got.RefreshSeconds, want.RefreshSeconds)
	}
	if got.MonthlyBudgetDollars == nil {
		t.Fatal("MonthlyBudgetDollars = nil, want 125")
	}
	if *got.MonthlyBudgetDollars != budget {
		t.Errorf("MonthlyBudgetDollars = %v, want %v", *got.MonthlyBudgetDollars, budget)
	}

	// The file holds no secret, but it holds an email; keep it owner-only.
	info, err := os.Stat(filepath.Join(home, ".burnrate", "config.toml"))
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.toml mode = %o, want 600", perm)
	}
}

func TestOmittedBudgetStaysNil(t *testing.T) {
	withHome(t)

	// A config with no budget must load as nil, not 0 — nil means "trust the
	// API's limit", while 0 would mean "you have no budget at all" and would
	// render the HP bar permanently empty.
	if err := (Config{Email: "someone@example.com", RefreshSeconds: 60}).Save(); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.MonthlyBudgetDollars != nil {
		t.Errorf("MonthlyBudgetDollars = %v, want nil", *got.MonthlyBudgetDollars)
	}
}

func TestEnvironmentOverridesFile(t *testing.T) {
	withHome(t)

	base := 10.0
	if err := (Config{
		Email:                "file@example.com",
		MonthlyBudgetDollars: &base,
	}).Save(); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	t.Setenv("CURSOR_EMAIL", "env@example.com")
	t.Setenv("CURSOR_MONTHLY_BUDGET", "250.5")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.Email != "env@example.com" {
		t.Errorf("Email = %q, want the env value", got.Email)
	}
	if got.MonthlyBudgetDollars == nil || *got.MonthlyBudgetDollars != 250.5 {
		t.Errorf("MonthlyBudgetDollars = %v, want 250.5", got.MonthlyBudgetDollars)
	}
}

func TestBadBudgetEnvIsRejected(t *testing.T) {
	withHome(t)
	t.Setenv("CURSOR_MONTHLY_BUDGET", "not-a-number")

	if _, err := Load(); err == nil {
		t.Fatal("Load() = nil, want an error for a non-numeric budget")
	}
}

func TestAPIKeyFallsBackToEnvironment(t *testing.T) {
	withHome(t)
	t.Setenv(EnvAPIKey, "key-from-env")

	// This exercises the fallback only when the Keychain has no entry. On a
	// machine where the probe stored a real key, skip rather than assert
	// against the developer's own Keychain.
	if _, err := keyringGet(); err == nil {
		t.Skip("a real key is present in the Keychain; fallback path not exercised")
	}

	got, err := APIKey()
	if err != nil {
		t.Fatalf("APIKey() = %v", err)
	}
	if got != "key-from-env" {
		t.Errorf("APIKey() = %q, want the env value", got)
	}
}

func TestAPIKeyMissingIsActionable(t *testing.T) {
	withHome(t)
	t.Setenv(EnvAPIKey, "")

	if _, err := keyringGet(); err == nil {
		t.Skip("a real key is present in the Keychain; missing-key path not exercised")
	}

	_, err := APIKey()
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("APIKey() = %v, want ErrNoKey", err)
	}
}

func TestSetAPIKeyRejectsEmpty(t *testing.T) {
	if err := SetAPIKey(""); err == nil {
		t.Fatal("SetAPIKey(\"\") = nil, want an error")
	}
}

// The buddy and machine save together as one arrangement.
func TestSetLookPersistsTheWholeCombo(t *testing.T) {
	withHome(t)

	if err := (Config{Email: "someone@example.com", RefreshSeconds: 120}).Save(); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if err := SetLook("capybara", "token-factory", true); err != nil {
		t.Fatalf("SetLook() = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.Buddy != "capybara" {
		t.Errorf("Buddy = %q, want capybara", got.Buddy)
	}
	if got.Machine != "token-factory" {
		t.Errorf("Machine = %q, want token-factory", got.Machine)
	}
	if !got.MachineOn {
		t.Error("MachineOn = false, want true")
	}
	// Saving a look must not discard the rest of the file.
	if got.Email != "someone@example.com" {
		t.Errorf("Email = %q, want it preserved", got.Email)
	}
	if got.RefreshSeconds != 120 {
		t.Errorf("RefreshSeconds = %d, want it preserved", got.RefreshSeconds)
	}
}

func TestSetLookOverwritesPrevious(t *testing.T) {
	withHome(t)

	if err := SetLook("duck", "money-furnace", true); err != nil {
		t.Fatalf("SetLook() = %v", err)
	}
	if err := SetLook("chonk", "pumpjack", false); err != nil {
		t.Fatalf("SetLook() = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.Buddy != "chonk" || got.Machine != "pumpjack" {
		t.Errorf("look = %q + %q, want the last one saved", got.Buddy, got.Machine)
	}
	// Saving with the machine hidden must record that, not leave it stuck on.
	if got.MachineOn {
		t.Error("MachineOn = true, want it to follow the saved state")
	}
}

// A config with no buddy must load as empty, not as a bogus name — empty means
// "use the first in the catalog".
func TestBuddyOmittedStaysEmpty(t *testing.T) {
	withHome(t)

	if err := (Config{Email: "someone@example.com"}).Save(); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.Buddy != "" {
		t.Errorf("Buddy = %q, want empty", got.Buddy)
	}
}
