package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andregcab/burnrate/internal/stats"
)

func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func sample() stats.Snapshot {
	return stats.Snapshot{
		SpentCents:     6746,
		LimitCents:     30000,
		RemainingCents: 23254,
		FractionLeft:   0.775,
		CycleStart:     time.Now().Add(-48 * time.Hour),
		CycleEnd:       time.Now().Add(29 * 24 * time.Hour),
		TopModels: []stats.ModelUse{
			{Model: "gpt-5.6-sol-xhigh", Cents: 2815, Events: 17, Share: 0.45},
		},
		EventsConsidered: 147,
		FetchedAt:        time.Now(),
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withHome(t)

	want := sample()
	if err := Save(want); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got.SpentCents != want.SpentCents || got.LimitCents != want.LimitCents {
		t.Errorf("figures = %d/%d, want %d/%d",
			got.SpentCents, got.LimitCents, want.SpentCents, want.LimitCents)
	}
	if len(got.TopModels) != 1 || got.TopModels[0].Model != "gpt-5.6-sol-xhigh" {
		t.Errorf("TopModels = %+v, want the model list preserved", got.TopModels)
	}
}

// A cached snapshot is never current. Marking it here rather than leaving it to
// callers means old numbers can never be presented as live.
func TestLoadAlwaysMarksStale(t *testing.T) {
	withHome(t)

	fresh := sample()
	fresh.Stale = false
	if err := Save(fresh); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if !got.Stale {
		t.Error("Load() returned a snapshot not marked stale")
	}
}

func TestLoadWithNoCache(t *testing.T) {
	withHome(t)
	if _, err := Load(); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load() = %v, want ErrNoCache", err)
	}
}

// A corrupt cache must read as absent, not as an error that blocks startup.
func TestCorruptCacheReadsAsAbsent(t *testing.T) {
	home := withHome(t)
	dir := filepath.Join(home, ".burnrate")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cache.json"),
		[]byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load() = %v, want ErrNoCache for a corrupt file", err)
	}
}

// A snapshot older than a billing cycle describes a budget that has since
// reset. Showing it, even marked stale, would be wrong rather than merely old.
func TestStaleBeyondMaxAgeIsDiscarded(t *testing.T) {
	home := withHome(t)
	if err := Save(sample()); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	path := filepath.Join(home, ".burnrate", "cache.json")
	old := time.Now().Add(-MaxAge - time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	// The age check reads the recorded timestamp, not the file mtime, so
	// rewrite the envelope with an old savedAt.
	buf, _ := os.ReadFile(path)
	stale := string(buf)
	if len(stale) == 0 {
		t.Fatal("cache file is empty")
	}
	if err := os.WriteFile(path, []byte(`{"savedAt":"2020-01-01T00:00:00Z","snapshot":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load() = %v, want ErrNoCache for an expired cache", err)
	}
}

func TestCacheIsOwnerOnly(t *testing.T) {
	home := withHome(t)
	if err := Save(sample()); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	info, err := os.Stat(filepath.Join(home, ".burnrate", "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %o, want 600 (it holds spending figures)", perm)
	}
}

func TestClearRemovesTheCache(t *testing.T) {
	withHome(t)
	if err := Save(sample()); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if err := Clear(); err != nil {
		t.Fatalf("Clear() = %v", err)
	}
	if _, err := Load(); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load() after Clear() = %v, want ErrNoCache", err)
	}
	// Clearing twice is not an error.
	if err := Clear(); err != nil {
		t.Errorf("second Clear() = %v", err)
	}
}
