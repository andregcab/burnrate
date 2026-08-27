package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withHome points HOME at a temp dir so tests never touch the real ~/.burnrate.
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
			if got := (Config{RefreshSeconds: tt.seconds}).Refresh(); got != tt.want {
				t.Errorf("Refresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	withHome(t)
	if _, err := Load(); err != nil {
		t.Fatalf("Load() on a missing file returned %v, want nil", err)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	home := withHome(t)

	budget := 125.0
	want := Config{
		MonthlyBudgetDollars: &budget,
		TeamID:               1234567,
		RefreshSeconds:       300,
	}
	if err := want.Save(); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.TeamID != want.TeamID {
		t.Errorf("TeamID = %d, want %d", got.TeamID, want.TeamID)
	}
	if got.RefreshSeconds != want.RefreshSeconds {
		t.Errorf("RefreshSeconds = %d, want %d", got.RefreshSeconds, want.RefreshSeconds)
	}
	if got.MonthlyBudgetDollars == nil || *got.MonthlyBudgetDollars != budget {
		t.Errorf("MonthlyBudgetDollars = %v, want %v", got.MonthlyBudgetDollars, budget)
	}

	info, err := os.Stat(filepath.Join(home, ".burnrate", "config.toml"))
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.toml mode = %o, want 600", perm)
	}
}

// Nil means "trust the API's limit"; zero would mean "you have no budget" and
// would render the gauge permanently empty.
func TestOmittedBudgetStaysNil(t *testing.T) {
	withHome(t)

	if err := (Config{TeamID: 1, RefreshSeconds: 60}).Save(); err != nil {
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
	if err := (Config{MonthlyBudgetDollars: &base}).Save(); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	t.Setenv("CURSOR_MONTHLY_BUDGET", "250.5")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
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

// The buddy and machine save together as one arrangement.
func TestSetLookPersistsTheWholeCombo(t *testing.T) {
	withHome(t)

	if err := (Config{TeamID: 1234567, RefreshSeconds: 120}).Save(); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if err := SetLook("capybara", "token-factory", true); err != nil {
		t.Fatalf("SetLook() = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.Buddy != "capybara" || got.Machine != "token-factory" || !got.MachineOn {
		t.Errorf("look = %q + %q (on=%v), want capybara + token-factory (on=true)",
			got.Buddy, got.Machine, got.MachineOn)
	}
	// Saving a look must not discard the rest of the file.
	if got.TeamID != 1234567 || got.RefreshSeconds != 120 {
		t.Errorf("other settings lost: TeamID=%d RefreshSeconds=%d",
			got.TeamID, got.RefreshSeconds)
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

func TestBuddyOmittedStaysEmpty(t *testing.T) {
	withHome(t)

	if err := (Config{TeamID: 1}).Save(); err != nil {
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

func TestSessionCookieMissingIsActionable(t *testing.T) {
	withHome(t)
	t.Setenv(EnvCookie, "")

	// Skip when a real cookie is in the Keychain rather than asserting against
	// — or clobbering — the developer's own credentials.
	if _, err := keyringGet(); err == nil {
		t.Skip("a real cookie is present in the Keychain")
	}
	if _, err := SessionCookie(); !errors.Is(err, ErrNoCookie) {
		t.Fatalf("SessionCookie() = %v, want ErrNoCookie", err)
	}
}

func TestSessionCookieFallsBackToEnvironment(t *testing.T) {
	withHome(t)
	t.Setenv(EnvCookie, "user_1::jwt.body.sig")

	if _, err := keyringGet(); err == nil {
		t.Skip("a real cookie is present in the Keychain")
	}
	got, err := SessionCookie()
	if err != nil {
		t.Fatalf("SessionCookie() = %v", err)
	}
	if got != "user_1::jwt.body.sig" {
		t.Errorf("SessionCookie() = %q, want the env value", got)
	}
}

func TestSetSessionCookieRejectsEmpty(t *testing.T) {
	if err := SetSessionCookie(""); err == nil {
		t.Fatal(`SetSessionCookie("") = nil, want an error`)
	}
}
