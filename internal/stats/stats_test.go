package stats

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/andregcab/burnrate/internal/cursor"
)

var (
	cycleStart = time.Date(2026, 8, 25, 0, 14, 35, 0, time.UTC)
	cycleEnd   = time.Date(2026, 9, 25, 0, 14, 35, 0, time.UTC)
)

// ev builds an event at an offset from the cycle start. Timestamps are epoch
// millis encoded as a string, matching what the API actually returns.
func ev(model string, cents float64, kind string, offset time.Duration) cursor.Event {
	return cursor.Event{
		Timestamp:    strconv.FormatInt(cycleStart.Add(offset).UnixMilli(), 10),
		Model:        model,
		Kind:         kind,
		ChargedCents: cents,
		IsChargeable: kind != cursor.KindErroredNotCharged,
	}
}

func summary(used, limit int) *cursor.Summary {
	s := &cursor.Summary{
		BillingCycleStart: cycleStart,
		BillingCycleEnd:   cycleEnd,
	}
	s.IndividualUsage.Overall = cursor.Bucket{
		Enabled: true, Used: used, Limit: limit, Remaining: limit - used,
	}
	return s
}

// The first trap from probe/FINDINGS.md: the API returns events from previous
// billing cycles. Real numbers were 147 in-cycle of 1457 returned; summing all
// of them overstated spend by 2.2x.
func TestBuildExcludesEventsBeforeCycleStart(t *testing.T) {
	events := []cursor.Event{
		ev("gpt-5.6-sol-xhigh", 100, cursor.KindUsageBased, 1*time.Hour),
		ev("gpt-5.6-sol-xhigh", 50, cursor.KindUsageBased, 2*time.Hour),
		// Previous cycle. Must not count.
		ev("gpt-5.6-sol-xhigh", 9999, cursor.KindUsageBased, -72*time.Hour),
		ev("claude-opus-5-thinking-high", 5000, cursor.KindUsageBased, -1*time.Minute),
	}

	got := Build(summary(150, 30000), events, 5, cycleStart.Add(3*time.Hour))

	if got.EventsConsidered != 2 {
		t.Errorf("EventsConsidered = %d, want 2 (out-of-cycle events leaked in)", got.EventsConsidered)
	}
	if math.Abs(got.ModelSpendCents-150) > 0.001 {
		t.Errorf("ModelSpendCents = %v, want 150", got.ModelSpendCents)
	}
	if len(got.TopModels) != 1 {
		t.Fatalf("TopModels has %d entries, want 1", len(got.TopModels))
	}
	if got.TopModels[0].Model != "gpt-5.6-sol-xhigh" {
		t.Errorf("TopModels[0] = %q, want gpt-5.6-sol-xhigh", got.TopModels[0].Model)
	}
}

// The second trap: IsChargeable is true for INCLUDED_IN_BUSINESS events that are
// never billed. Filtering on it gave 14806c against an authoritative 6746c.
func TestBuildCountsOnlyUsageBasedKind(t *testing.T) {
	events := []cursor.Event{
		ev("gpt-5.6-sol-xhigh", 100, cursor.KindUsageBased, time.Hour),
		// Chargeable, but covered by the plan and not billed.
		ev("claude-opus-5-thinking-high", 4691, cursor.KindIncludedInBusiness, time.Hour),
		ev("gpt-5.6-luna-medium", 12, cursor.KindErroredNotCharged, time.Hour),
	}

	got := Build(summary(100, 30000), events, 5, cycleStart.Add(2*time.Hour))

	if got.EventsConsidered != 1 {
		t.Errorf("EventsConsidered = %d, want 1", got.EventsConsidered)
	}
	if math.Abs(got.ModelSpendCents-100) > 0.001 {
		t.Errorf("ModelSpendCents = %v, want 100 (non-billed kinds leaked in)", got.ModelSpendCents)
	}
	for _, m := range got.TopModels {
		if m.Model == "claude-opus-5-thinking-high" {
			t.Error("INCLUDED_IN_BUSINESS model appeared in TopModels")
		}
	}
}

// With both filters applied, the event sum should track the authoritative spend
// figure. This is the regression form of the M0 reconciliation.
func TestBuildReconcilesWithAuthoritativeSpend(t *testing.T) {
	var events []cursor.Event
	for i := 0; i < 10; i++ {
		events = append(events, ev("gpt-5.6-sol-xhigh", 674.6, cursor.KindUsageBased,
			time.Duration(i)*time.Hour))
	}

	got := Build(summary(6746, 30000), events, 5, cycleStart.Add(11*time.Hour))

	drift := math.Abs(got.ModelSpendCents-float64(got.SpentCents)) / float64(got.SpentCents)
	if drift > 0.02 {
		t.Errorf("model spend %v vs authoritative %d is %.1f%% apart, want under 2%%",
			got.ModelSpendCents, got.SpentCents, drift*100)
	}
}

func TestFractionLeftAndUnlimited(t *testing.T) {
	tests := []struct {
		name        string
		used, limit int
		unlimited   bool
		want        float64
	}{
		{"fresh cycle", 0, 30000, false, 1},
		{"real sample", 6751, 30000, false, 0.7749666666666667},
		{"exhausted", 30000, 30000, false, 0},
		{"overspent clamps to zero", 31000, 30000, false, 0},
		{"no limit reads as full", 500, 0, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := summary(tt.used, tt.limit)
			s.IsUnlimited = tt.unlimited
			got := Build(s, nil, 5, cycleStart)
			if math.Abs(got.FractionLeft-tt.want) > 0.0001 {
				t.Errorf("FractionLeft = %v, want %v", got.FractionLeft, tt.want)
			}
		})
	}
}

func TestTopModelsSortedAndTruncated(t *testing.T) {
	events := []cursor.Event{
		ev("cheap", 10, cursor.KindUsageBased, time.Hour),
		ev("priciest", 900, cursor.KindUsageBased, time.Hour),
		ev("middle", 400, cursor.KindUsageBased, time.Hour),
		ev("second", 700, cursor.KindUsageBased, time.Hour),
		ev("fourth", 100, cursor.KindUsageBased, time.Hour),
		ev("last", 1, cursor.KindUsageBased, time.Hour),
	}

	got := Build(summary(2111, 30000), events, 4, cycleStart.Add(2*time.Hour))

	if len(got.TopModels) != 4 {
		t.Fatalf("TopModels has %d entries, want 4", len(got.TopModels))
	}
	want := []string{"priciest", "second", "middle", "fourth"}
	for i, w := range want {
		if got.TopModels[i].Model != w {
			t.Errorf("TopModels[%d] = %q, want %q", i, got.TopModels[i].Model, w)
		}
	}

	var sum float64
	for _, m := range got.TopModels {
		sum += m.Share
	}
	// Shares are of total spend, so the truncated top-4 sums to just under 1.
	if sum > 1.0001 {
		t.Errorf("shares sum to %v, want <= 1", sum)
	}
}

// Equal-spend models must not shuffle between frames, or the HUD becomes
// unreadable as rows swap on every refresh.
func TestTiedModelsSortStably(t *testing.T) {
	events := []cursor.Event{
		ev("zebra", 100, cursor.KindUsageBased, time.Hour),
		ev("alpha", 100, cursor.KindUsageBased, time.Hour),
		ev("mango", 100, cursor.KindUsageBased, time.Hour),
	}
	for i := 0; i < 20; i++ {
		got := Build(summary(300, 30000), events, 5, cycleStart.Add(2*time.Hour))
		if got.TopModels[0].Model != "alpha" || got.TopModels[2].Model != "zebra" {
			t.Fatalf("run %d ordered %q,%q,%q — ties are not stable",
				i, got.TopModels[0].Model, got.TopModels[1].Model, got.TopModels[2].Model)
		}
	}
}

func TestMalformedTimestampIsSkippedNotFatal(t *testing.T) {
	events := []cursor.Event{
		{Timestamp: "not-a-number", Model: "junk", Kind: cursor.KindUsageBased, ChargedCents: 500},
		ev("good", 25, cursor.KindUsageBased, time.Hour),
	}

	got := Build(summary(25, 30000), events, 5, cycleStart.Add(2*time.Hour))

	if got.EventsConsidered != 1 {
		t.Errorf("EventsConsidered = %d, want 1", got.EventsConsidered)
	}
	if len(got.TopModels) != 1 || got.TopModels[0].Model != "good" {
		t.Errorf("TopModels = %+v, want only the well-formed event", got.TopModels)
	}
}

func TestBurnRateAndProjection(t *testing.T) {
	s := Build(summary(6746, 30000), nil, 5, cycleStart)
	now := cycleStart.Add(48 * time.Hour) // two days in

	rate := s.BurnRateCentsPerDay(now)
	if math.Abs(rate-3373) > 1 {
		t.Errorf("BurnRateCentsPerDay = %v, want ~3373 (6746 over 2 days)", rate)
	}

	// A 31-day cycle at that rate blows well past the $300 cap.
	proj := s.ProjectedCents(now)
	if proj <= float64(s.LimitCents) {
		t.Errorf("ProjectedCents = %v, want more than the %d limit", proj, s.LimitCents)
	}

	when := s.RunsOutOn(now)
	if when.IsZero() {
		t.Fatal("RunsOutOn returned zero, want a date inside the cycle")
	}
	if when.Before(now) || when.After(cycleEnd) {
		t.Errorf("RunsOutOn = %v, want between %v and %v", when, now, cycleEnd)
	}
}

// Early in a cycle a single expensive request would project an absurd monthly
// total, so the rate is suppressed until enough time has elapsed.
func TestBurnRateSuppressedVeryEarlyInCycle(t *testing.T) {
	s := Build(summary(500, 30000), nil, 5, cycleStart)
	if got := s.BurnRateCentsPerDay(cycleStart.Add(5 * time.Minute)); got != 0 {
		t.Errorf("BurnRateCentsPerDay = %v five minutes in, want 0", got)
	}
}

func TestRunsOutOnZeroWhenComfortablyWithinBudget(t *testing.T) {
	s := Build(summary(100, 30000), nil, 5, cycleStart)
	if when := s.RunsOutOn(cycleStart.Add(48 * time.Hour)); !when.IsZero() {
		t.Errorf("RunsOutOn = %v, want zero when spend is trivial", when)
	}
}

func TestDaysLeftFloorsAtZero(t *testing.T) {
	s := Build(summary(0, 30000), nil, 5, cycleStart)
	if got := s.DaysLeft(cycleEnd.Add(72 * time.Hour)); got != 0 {
		t.Errorf("DaysLeft = %d past cycle end, want 0", got)
	}
	if got := s.DaysLeft(cycleStart); got < 30 || got > 31 {
		t.Errorf("DaysLeft = %d at cycle start, want 30 or 31", got)
	}
}
