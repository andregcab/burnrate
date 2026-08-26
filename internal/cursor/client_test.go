package cursor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serve spins up a stand-in for the dashboard backend and returns a Client
// pointed at it, plus a record of the requests it received.
func serve(t *testing.T, handler http.HandlerFunc) (*Client, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Clone(r.Context()))
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New("user_123::jwt.payload.sig", 999, WithBaseURL(srv.URL)), &got
}

// Origin and Referer are the difference between 403 and 200 on these routes.
// Dropping them breaks everything, and the failure looks like a permissions
// problem rather than a missing header, so it is worth pinning explicitly.
func TestRequestsCarryCSRFHeaders(t *testing.T) {
	c, got := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"billingCycleStart":"2026-08-25T00:00:00Z"}`)
	})

	if _, err := c.UsageSummary(context.Background()); err != nil {
		t.Fatalf("UsageSummary() = %v", err)
	}

	req := (*got)[0]
	for header, want := range map[string]string{
		"Origin":  BaseURL,
		"Referer": BaseURL + "/dashboard",
	} {
		if v := req.Header.Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
	if ck := req.Header.Get("Cookie"); !strings.Contains(ck, "WorkosCursorSessionToken=") {
		t.Errorf("Cookie = %q, want the session token", ck)
	}
	if ua := req.Header.Get("User-Agent"); ua == "" {
		t.Error("User-Agent is empty; these routes expect a browser")
	}
}

// An expired session redirects to the single-page app, which answers 200 with
// HTML. Without detecting that, it surfaces as a confusing JSON parse error
// instead of "your session expired".
func TestHTMLResponseIsTreatedAsExpiredSession(t *testing.T) {
	c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<!DOCTYPE html><html><head><title>Cursor</title></head></html>")
	})

	_, err := c.UsageSummary(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("UsageSummary() = %v, want ErrUnauthorized", err)
	}
}

func TestStatusCodesMapToTypedErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized},
		{"forbidden", http.StatusForbidden, ErrForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			})
			if _, err := c.UsageSummary(context.Background()); !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

// A server error must not masquerade as an auth problem — that would send
// someone re-pasting a cookie that was fine.
func TestServerErrorIsNotReportedAsAuthFailure(t *testing.T) {
	c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	})

	_, err := c.UsageSummary(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) {
		t.Errorf("500 reported as an auth error: %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not mention the status code", err)
	}
}

func TestUsageSummaryParsesTheRealShape(t *testing.T) {
	c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"billingCycleStart":"2026-08-25T00:14:35.682Z",
			"billingCycleEnd":"2026-09-25T00:14:35.682Z",
			"membershipType":"enterprise",
			"isUnlimited":false,
			"individualUsage":{"overall":{"enabled":true,"used":6751,"limit":30000,"remaining":23249}},
			"teamUsage":{"onDemand":{"enabled":true,"used":7723421,"limit":10000000}}
		}`)
	})

	sum, err := c.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("UsageSummary() = %v", err)
	}
	if got := sum.IndividualUsage.Overall.Used; got != 6751 {
		t.Errorf("Used = %d, want 6751", got)
	}
	if got := sum.IndividualUsage.Overall.Limit; got != 30000 {
		t.Errorf("Limit = %d, want 30000", got)
	}
	if !sum.BillingCycleStart.Equal(time.Date(2026, 8, 25, 0, 14, 35, 682000000, time.UTC)) {
		t.Errorf("BillingCycleStart = %v", sum.BillingCycleStart)
	}
}

// The event walk must stop at the cycle boundary. The endpoint returns recent
// events regardless of billing cycle, and on a two-day-old cycle only 147 of
// 1457 belonged to it — summing the rest overstated spend by 2.2x.
func TestUsageEventsSinceStopsAtTheCycleBoundary(t *testing.T) {
	cycleStart := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

	page := 0
	c, got := serve(t, func(w http.ResponseWriter, r *http.Request) {
		page++
		// One full page inside the cycle, then a page that crosses out of it.
		var b strings.Builder
		b.WriteString(`{"totalUsageEventsCount":2000,"usageEventsDisplay":[`)
		for i := 0; i < MaxPageSize; i++ {
			ts := cycleStart.Add(time.Duration(i) * time.Second)
			if page > 1 {
				ts = cycleStart.Add(-time.Duration(i+1) * time.Hour)
			}
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b,
				`{"timestamp":"%d","model":"m","kind":"%s","chargedCents":1}`,
				ts.UnixMilli(), KindUsageBased)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	})

	events, err := c.UsageEventsSince(context.Background(), cycleStart, 20)
	if err != nil {
		t.Fatalf("UsageEventsSince() = %v", err)
	}
	if len(events) != MaxPageSize {
		t.Errorf("collected %d events, want %d (it kept reading past the boundary)",
			len(events), MaxPageSize)
	}
	if n := len(*got); n != 2 {
		t.Errorf("made %d requests, want 2 (stop as soon as events go out of cycle)", n)
	}
}

// A short page is the last page; asking for another wastes a request.
func TestUsageEventsSinceStopsOnAShortPage(t *testing.T) {
	c, got := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"totalUsageEventsCount":1,"usageEventsDisplay":[
			{"timestamp":"%d","model":"m","kind":"%s","chargedCents":5}]}`,
			time.Now().UnixMilli(), KindUsageBased)
	})

	events, err := c.UsageEventsSince(context.Background(),
		time.Now().Add(-24*time.Hour), 20)
	if err != nil {
		t.Fatalf("UsageEventsSince() = %v", err)
	}
	if len(events) != 1 {
		t.Errorf("collected %d events, want 1", len(events))
	}
	if n := len(*got); n != 1 {
		t.Errorf("made %d requests, want 1", n)
	}
}

// maxPages is a backstop against a pagination bug spinning forever.
func TestUsageEventsSinceRespectsMaxPages(t *testing.T) {
	c, got := serve(t, func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"totalUsageEventsCount":99999,"usageEventsDisplay":[`)
		for i := 0; i < MaxPageSize; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"timestamp":"%d","model":"m","kind":"%s","chargedCents":1}`,
				time.Now().UnixMilli(), KindUsageBased)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	})

	if _, err := c.UsageEventsSince(context.Background(),
		time.Now().Add(-time.Hour), 3); err != nil {
		t.Fatalf("UsageEventsSince() = %v", err)
	}
	if n := len(*got); n != 3 {
		t.Errorf("made %d requests, want the 3-page cap", n)
	}
}

func TestUsageEventsPageClampsItsArguments(t *testing.T) {
	// Read the body inside the handler: a cloned request's body is already
	// drained by the time the test sees it.
	var sent string
	c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sent = string(b)
		fmt.Fprint(w, `{"totalUsageEventsCount":0,"usageEventsDisplay":[]}`)
	})

	if _, err := c.UsageEventsPage(context.Background(), -5, 99999); err != nil {
		t.Fatalf("UsageEventsPage() = %v", err)
	}
	if !strings.Contains(sent, `"page":1`) {
		t.Errorf("body %q did not clamp page to 1", sent)
	}
	if !strings.Contains(sent, fmt.Sprintf(`"pageSize":%d`, MaxPageSize)) {
		t.Errorf("body %q did not clamp pageSize to %d", sent, MaxPageSize)
	}
	if !strings.Contains(sent, `"teamId":999`) {
		t.Errorf("body %q is missing the team id", sent)
	}
}

func TestEventTimeParsesStringMillis(t *testing.T) {
	ev := Event{Timestamp: "1787773712502"}
	got, err := ev.Time()
	if err != nil {
		t.Fatalf("Time() = %v", err)
	}
	if want := time.UnixMilli(1787773712502); !got.Equal(want) {
		t.Errorf("Time() = %v, want %v", got, want)
	}

	if _, err := (Event{Timestamp: "not-a-number"}).Time(); err == nil {
		t.Error("Time() accepted a malformed timestamp")
	}
}

// Kind, not IsChargeable, decides what counts. IsChargeable is true for
// INCLUDED_IN_BUSINESS events that are never billed; filtering on it overstated
// a real cycle by more than 2x.
func TestBilledUsesKindNotChargeableFlag(t *testing.T) {
	tests := []struct {
		kind       string
		chargeable bool
		want       bool
	}{
		{KindUsageBased, true, true},
		{KindIncludedInBusiness, true, false},
		{KindErroredNotCharged, false, false},
	}
	for _, tt := range tests {
		ev := Event{Kind: tt.kind, IsChargeable: tt.chargeable}
		if got := ev.Billed(); got != tt.want {
			t.Errorf("Billed() for %s = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestUserIDDecodesPercentEncodedSeparator(t *testing.T) {
	tests := []struct{ in, want string }{
		{"user_01ABC::eyJhbGci.body.sig", "user_01ABC"},
		{"user_01ABC%3A%3AeyJhbGci.body.sig", "user_01ABC"},
		{"no-separator-here", ""},
	}
	for _, tt := range tests {
		if got := UserID(tt.in); got != tt.want {
			t.Errorf("UserID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
