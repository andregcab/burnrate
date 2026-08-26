// Package store persists the last good snapshot.
//
// It exists for two reasons, neither of which is saving API calls: a refresh is
// only one or two requests, so there is little to save. What it buys is that
// the HUD has something to draw the instant it starts instead of a spinner, and
// that a dropped network shows yesterday's numbers marked STALE rather than an
// error card.
//
// Deliberately not implemented: incremental event accumulation. The original
// plan had a per-model rollup with a high-water mark so each refresh fetched
// only new events. At observed volumes a whole cycle fits in one or two pages,
// so that machinery would add dedupe logic and a class of silent
// double-counting bugs in exchange for nothing measurable.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/andregcab/burnrate/internal/config"
	"github.com/andregcab/burnrate/internal/stats"
)

// ErrNoCache means nothing has been stored yet.
var ErrNoCache = errors.New("no cached snapshot")

// MaxAge is how old a cached snapshot may be before it is ignored.
//
// A cycle is about a month, so a snapshot older than that describes a budget
// that has since reset — showing it, even marked stale, would be misleading
// rather than merely out of date.
const MaxAge = 31 * 24 * time.Hour

type envelope struct {
	SavedAt  time.Time      `json:"savedAt"`
	Snapshot stats.Snapshot `json:"snapshot"`
}

// Path is where the cache lives.
func Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache.json"), nil
}

// Save writes a snapshot, replacing any previous one.
//
// Failure is not fatal to the caller: a cache that cannot be written costs a
// slower start next time and nothing else.
func Save(s stats.Snapshot) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	buf, err := json.Marshal(envelope{SavedAt: time.Now(), Snapshot: s})
	if err != nil {
		return fmt.Errorf("encoding cache: %w", err)
	}

	// Write and rename, so an interrupted save cannot leave a truncated file
	// that fails to parse on the next start.
	tmp, err := os.CreateTemp(dir, "cache-*.json")
	if err != nil {
		return fmt.Errorf("creating temp cache: %w", err)
	}
	defer os.Remove(tmp.Name())

	// The cache holds spending figures, so keep it owner-only.
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("securing temp cache: %w", err)
	}
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp cache: %w", err)
	}

	path := filepath.Join(dir, "cache.json")
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("saving %s: %w", path, err)
	}
	return nil
}

// Load returns the stored snapshot, always marked stale.
//
// Stale is set here rather than by the caller because a cached snapshot is
// never current by definition, and forgetting to mark it would present old
// numbers as live — the one failure mode this package must not have.
func Load() (stats.Snapshot, error) {
	path, err := Path()
	if err != nil {
		return stats.Snapshot{}, err
	}

	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return stats.Snapshot{}, ErrNoCache
		}
		return stats.Snapshot{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var env envelope
	if err := json.Unmarshal(buf, &env); err != nil {
		// A corrupt cache is not worth reporting upward; treat it as absent.
		return stats.Snapshot{}, ErrNoCache
	}
	if time.Since(env.SavedAt) > MaxAge {
		return stats.Snapshot{}, ErrNoCache
	}

	env.Snapshot.Stale = true
	return env.Snapshot, nil
}

// Clear removes the cache. A missing file is not an error.
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", path, err)
	}
	return nil
}
