package provider

import (
	"context"
	"errors"

	"github.com/andregcab/burnrate/internal/cursor"
	"github.com/andregcab/burnrate/internal/stats"
	"github.com/andregcab/burnrate/internal/store"
)

// Cached wraps a Provider so a dropped network shows the last good numbers
// marked STALE rather than an error card.
type Cached struct {
	Inner Provider
}

// NewCached wraps a provider with the on-disk snapshot cache.
func NewCached(inner Provider) *Cached { return &Cached{Inner: inner} }

// Snapshot fetches fresh data, falling back to the cache on transient failure.
//
// Authentication failures are deliberately NOT absorbed. An expired session is
// not transient — it needs the user to act — and quietly showing last week's
// numbers instead of saying so would hide the one error that requires a
// response. Everything else (a dropped wifi, a 500, a timeout) is worth riding
// out on cached data.
func (c *Cached) Snapshot(ctx context.Context) (stats.Snapshot, error) {
	fresh, err := c.Inner.Snapshot(ctx)
	if err == nil {
		// A cache write failure costs a slower start next time and nothing
		// else, so it must not fail the fetch that just succeeded.
		_ = store.Save(fresh)
		return fresh, nil
	}

	if errors.Is(err, cursor.ErrUnauthorized) {
		return stats.Snapshot{}, err
	}

	cached, cerr := store.Load()
	if cerr != nil {
		return stats.Snapshot{}, err // report the real failure, not the miss
	}
	return cached, nil
}

// Cachedmarker keeps the interface assertion honest at compile time.
var _ Provider = (*Cached)(nil)
