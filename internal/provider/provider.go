// Package provider is the seam between the UI and wherever usage data comes
// from.
//
// The official Admin API is unavailable to non-admin team members, so the
// shipping implementation reads the dashboard backend with a session cookie.
// That surface is undocumented and can break. Keeping it behind an interface
// means replacing it later is a one-line change in main rather than a rewrite.
package provider

import (
	"context"
	"time"

	"github.com/andregcab/burnrate/internal/cursor"
	"github.com/andregcab/burnrate/internal/stats"
)

// Provider produces snapshots.
type Provider interface {
	Snapshot(ctx context.Context) (stats.Snapshot, error)
}

// Session reads from the Cursor dashboard backend using a browser session
// cookie.
type Session struct {
	Client *cursor.Client
	TopN   int

	// MaxPages bounds the event walk. Twenty pages of 1000 is far more than a
	// cycle ever contains; it exists so a pagination bug cannot spin forever.
	MaxPages int
}

// NewSession builds a session-backed provider.
func NewSession(c *cursor.Client, topN int) *Session {
	if topN <= 0 {
		topN = 5
	}
	return &Session{Client: c, TopN: topN, MaxPages: 20}
}

// Snapshot fetches the usage summary and this cycle's events.
//
// A failed event fetch is downgraded to a partial snapshot rather than an
// error: the HP bar is the headline number, and losing it because the model
// breakdown was unavailable would be the wrong trade.
func (s *Session) Snapshot(ctx context.Context) (stats.Snapshot, error) {
	sum, err := s.Client.UsageSummary(ctx)
	if err != nil {
		return stats.Snapshot{}, err
	}

	events, err := s.Client.UsageEventsSince(ctx, sum.BillingCycleStart, s.MaxPages)
	if err != nil {
		snap := stats.Build(sum, nil, s.TopN, time.Now())
		return snap, nil
	}

	return stats.Build(sum, events, s.TopN, time.Now()), nil
}

// Static replays a fixed snapshot. It backs --demo, so the HUD and its
// animations can be developed, demoed, and screenshotted with no credentials
// and no API traffic.
type Static struct {
	Snap stats.Snapshot

	// Drain, when set, walks the HP bar down over this duration so the low-HP
	// alarm and empty states are reachable without waiting a billing cycle.
	Drain time.Duration
	start time.Time
}

// Snapshot returns the canned snapshot, optionally draining.
func (d *Static) Snapshot(_ context.Context) (stats.Snapshot, error) {
	if d.start.IsZero() {
		d.start = time.Now()
	}
	s := d.Snap
	s.FetchedAt = time.Now()

	if d.Drain > 0 && s.LimitCents > 0 {
		elapsed := time.Since(d.start)
		frac := 1 - float64(elapsed)/float64(d.Drain)
		if frac < 0 {
			frac = 0
		}
		s.RemainingCents = int(float64(s.LimitCents) * frac)
		s.SpentCents = s.LimitCents - s.RemainingCents
		s.FractionLeft = frac
	}
	return s, nil
}
