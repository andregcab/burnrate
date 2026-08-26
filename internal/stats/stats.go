// Package stats turns raw Cursor responses into the single struct every
// renderer consumes.
//
// Snapshot is the contract between the data layer and the UI: the TUI and the
// desktop widget both read it, and neither knows or cares where it came from.
package stats

import (
	"sort"
	"time"

	"github.com/andregcab/burnrate/internal/cursor"
)

// Snapshot is everything the HUD needs for one frame.
type Snapshot struct {
	// SpentCents and LimitCents come straight from usage-summary and are
	// authoritative. RemainingCents is reported by the API rather than derived,
	// so it stays correct even if the other two disagree.
	SpentCents     int
	LimitCents     int
	RemainingCents int

	// FractionLeft is 0..1, for the HP bar.
	FractionLeft float64

	// Unlimited means there is no cap, so there is no meaningful bar to draw.
	Unlimited bool

	CycleStart time.Time
	CycleEnd   time.Time

	// TopModels is sorted by spend, descending.
	TopModels []ModelUse

	// EventsConsidered is how many in-cycle billed events fed TopModels. Zero
	// with nonzero spend means the breakdown is incomplete, not that you idled.
	EventsConsidered int

	// BurstCents is spend in the last BurstWindow. This is the *instantaneous*
	// rate, and it is what the machine animation reads.
	//
	// The cycle average is useless for that: two days into a cycle it barely
	// twitches when you fire up a max-mode agent, so an animation driven by it
	// would idle while you were burning hardest — meaningful-looking motion
	// that carries no information.
	BurstCents float64

	// BurstEvents is how many billed events landed in the same window.
	BurstEvents int

	// ModelSpendCents is the summed spend across TopModels. Compared against
	// SpentCents it is a self-check: a large gap means the aggregation drifted.
	ModelSpendCents float64

	FetchedAt time.Time

	// Stale marks a snapshot served from cache after a failed refresh.
	Stale bool
}

// BurstWindow is how far back BurstCents looks. Short enough to react while
// you are still watching, long enough that one request does not read as a
// sustained spree.
const BurstWindow = 15 * time.Minute

// BurstCentsPerDay extrapolates the burst window to a daily rate, so it can be
// compared against the cycle average on the same scale.
func (s Snapshot) BurstCentsPerDay() float64 {
	return s.BurstCents * (24 * time.Hour).Seconds() / BurstWindow.Seconds()
}

// EffectiveBurnCentsPerDay is the rate the machine actually reflects: whichever
// is higher of the instantaneous burst and the cycle average.
//
// Taking the max means the machine reacts immediately to a spree without
// falling asleep during steady sustained use.
func (s Snapshot) EffectiveBurnCentsPerDay(now time.Time) float64 {
	rate := s.BurstCentsPerDay()
	if avg := s.BurnRateCentsPerDay(now); avg > rate {
		rate = avg
	}
	return rate
}

// ActivityRate is 0..1, how hard the machine should be working right now.
func (s Snapshot) ActivityRate(now time.Time, fullScaleCentsPerDay float64) float64 {
	if fullScaleCentsPerDay <= 0 {
		return 0
	}
	f := s.EffectiveBurnCentsPerDay(now) / fullScaleCentsPerDay
	if f > 1 {
		return 1
	}
	if f < 0 {
		return 0
	}
	return f
}

// ModelUse is one model's share of the cycle.
type ModelUse struct {
	Model  string
	Cents  float64
	Events int
	// Share is this model's fraction of ModelSpendCents, 0..1.
	Share float64
}

// DaysLeft is whole days remaining in the billing cycle, floored at zero.
func (s Snapshot) DaysLeft(now time.Time) int {
	if s.CycleEnd.IsZero() {
		return 0
	}
	d := int(s.CycleEnd.Sub(now).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// BurnRateCentsPerDay is spend so far divided by elapsed cycle time.
func (s Snapshot) BurnRateCentsPerDay(now time.Time) float64 {
	if s.CycleStart.IsZero() {
		return 0
	}
	elapsed := now.Sub(s.CycleStart).Hours() / 24
	// Below a few hours the rate is noise — a single expensive request would
	// project to an absurd monthly total.
	if elapsed < 0.25 {
		return 0
	}
	return float64(s.SpentCents) / elapsed
}

// ProjectedCents estimates end-of-cycle spend at the current burn rate.
// Zero means "not enough of the cycle has elapsed to say".
func (s Snapshot) ProjectedCents(now time.Time) float64 {
	rate := s.BurnRateCentsPerDay(now)
	if rate == 0 || s.CycleEnd.IsZero() {
		return 0
	}
	total := s.CycleEnd.Sub(s.CycleStart).Hours() / 24
	return rate * total
}

// RunsOutOn estimates when spend would reach the limit at the current rate.
// The zero time means "not on this cycle's trajectory".
func (s Snapshot) RunsOutOn(now time.Time) time.Time {
	rate := s.BurnRateCentsPerDay(now)
	if rate <= 0 || s.Unlimited || s.LimitCents <= 0 {
		return time.Time{}
	}
	daysLeft := float64(s.RemainingCents) / rate
	when := now.Add(time.Duration(daysLeft * 24 * float64(time.Hour)))
	if !s.CycleEnd.IsZero() && when.After(s.CycleEnd) {
		return time.Time{} // comfortably within budget
	}
	return when
}

// Build assembles a Snapshot from a usage summary and the cycle's events.
//
// Two filters are applied and both are load-bearing (see probe/FINDINGS.md):
//
//   - Events older than the cycle start are dropped. The API returns recent
//     events regardless of cycle; on a two-day-old cycle only 147 of 1457
//     belonged to it, so skipping this overstates spend by more than 2x.
//   - Only USAGE_EVENT_KIND_USAGE_BASED events count. IsChargeable is true for
//     INCLUDED_IN_BUSINESS events that are never billed, so filtering on it
//     still overstates the total.
func Build(sum *cursor.Summary, events []cursor.Event, topN int, now time.Time) Snapshot {
	if topN <= 0 {
		topN = 5
	}

	b := sum.IndividualUsage.Overall
	s := Snapshot{
		SpentCents:     b.Used,
		LimitCents:     b.Limit,
		RemainingCents: b.Remaining,
		Unlimited:      sum.IsUnlimited || b.Limit <= 0,
		CycleStart:     sum.BillingCycleStart,
		CycleEnd:       sum.BillingCycleEnd,
		FetchedAt:      now,
	}

	if !s.Unlimited {
		s.FractionLeft = float64(b.Remaining) / float64(b.Limit)
		s.FractionLeft = clamp01(s.FractionLeft)
	} else {
		s.FractionLeft = 1
	}

	byModel := map[string]*ModelUse{}
	for _, ev := range events {
		if !ev.Billed() {
			continue
		}
		ts, err := ev.Time()
		if err != nil {
			continue
		}
		if !sum.BillingCycleStart.IsZero() && ts.Before(sum.BillingCycleStart) {
			continue
		}

		name := ev.Model
		if name == "" {
			name = "unknown"
		}
		m, ok := byModel[name]
		if !ok {
			m = &ModelUse{Model: name}
			byModel[name] = m
		}
		m.Cents += ev.ChargedCents
		m.Events++

		s.EventsConsidered++
		s.ModelSpendCents += ev.ChargedCents

		if now.Sub(ts) <= BurstWindow {
			s.BurstCents += ev.ChargedCents
			s.BurstEvents++
		}
	}

	models := make([]ModelUse, 0, len(byModel))
	for _, m := range byModel {
		if s.ModelSpendCents > 0 {
			m.Share = m.Cents / s.ModelSpendCents
		}
		models = append(models, *m)
	}

	// Sort by spend, then name, so equal-spend models do not shuffle between
	// frames — a HUD that reorders on every refresh is unreadable.
	sort.Slice(models, func(i, j int) bool {
		if models[i].Cents != models[j].Cents {
			return models[i].Cents > models[j].Cents
		}
		return models[i].Model < models[j].Model
	})

	if len(models) > topN {
		models = models[:topN]
	}
	s.TopModels = models
	return s
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
