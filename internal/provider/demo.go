package provider

import (
	"time"

	"github.com/andregcab/burnrate/internal/stats"
)

// DemoSnapshot is plausible fake data shaped like the real thing: mixed model
// name formats, an uneven spend distribution, a mid-cycle date.
//
// It exists so the HUD can be built and shown without a credential, which also
// makes it the safe thing to record a README GIF from.
func DemoSnapshot(now time.Time) stats.Snapshot {
	start := now.Add(-9 * 24 * time.Hour)
	end := start.Add(31 * 24 * time.Hour)

	models := []stats.ModelUse{
		{Model: "gpt-5.6-sol-xhigh", Cents: 2815, Events: 17},
		{Model: "gpt-5.6-luna-medium", Cents: 1585, Events: 87},
		{Model: "claude-opus-5-thinking-high", Cents: 708, Events: 24},
		{Model: "Cursor Grok 4.6 (Auto Balanced)", Cents: 701, Events: 27},
		{Model: "composer-2.5", Cents: 361, Events: 12},
	}
	var total float64
	for _, m := range models {
		total += m.Cents
	}
	for i := range models {
		models[i].Share = models[i].Cents / total
	}

	spent := int(total)
	limit := 30000

	return stats.Snapshot{
		SpentCents:       spent,
		LimitCents:       limit,
		RemainingCents:   limit - spent,
		FractionLeft:     float64(limit-spent) / float64(limit),
		CycleStart:       start,
		CycleEnd:         end,
		TopModels:        models,
		EventsConsidered: 167,
		ModelSpendCents:  total,
		FetchedAt:        now,
	}
}
