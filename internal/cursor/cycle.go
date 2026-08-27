package cursor

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// Billing cycle boundaries.
//
// usage-summary also reports a cycle, but its end is unreliable: it comes back
// as exactly start + 31 days, which is a computed default rather than a real
// boundary. This endpoint returns a clean midnight-UTC date that matches
// get-team-spend's nextCycleStart, and matches what the dashboard shows.
//
// Getting this wrong is not cosmetic. The cycle end drives the days-remaining
// figure and the "empty by" projection, so a wrong date tells you that you will
// run out before a reset that has in fact already happened.

// cycleResponse has epoch milliseconds encoded as strings.
type cycleResponse struct {
	StartMillis string `json:"startDateEpochMillis"`
	EndMillis   string `json:"endDateEpochMillis"`
}

// Cycle is a billing period.
type Cycle struct {
	Start time.Time
	End   time.Time
}

// BillingCycle fetches the authoritative cycle boundaries.
func (c *Client) BillingCycle(ctx context.Context) (Cycle, error) {
	var resp cycleResponse
	err := c.do(ctx, http.MethodPost, "/api/dashboard/get-monthly-billing-cycle",
		map[string]any{"teamId": c.teamID}, &resp)
	if err != nil {
		return Cycle{}, err
	}

	var cyc Cycle
	if t, ok := parseEpochMillis(resp.StartMillis); ok {
		cyc.Start = t
	}
	if t, ok := parseEpochMillis(resp.EndMillis); ok {
		cyc.End = t
	}
	return cyc, nil
}

func parseEpochMillis(s string) (time.Time, bool) {
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}
