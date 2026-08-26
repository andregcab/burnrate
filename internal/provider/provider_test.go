package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andregcab/burnrate/internal/cursor"
)

func summaryJSON(start, end time.Time, used, limit int) string {
	return fmt.Sprintf(`{
		"billingCycleStart":%q,"billingCycleEnd":%q,
		"isUnlimited":false,
		"individualUsage":{"overall":{"enabled":true,"used":%d,"limit":%d,"remaining":%d}}
	}`, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano),
		used, limit, limit-used)
}

// The gauge is the headline number. A failed event fetch must cost only the
// model breakdown, not the whole snapshot.
func TestSnapshotSurvivesAFailedEventFetch(t *testing.T) {
	start := time.Now().Add(-48 * time.Hour)
	end := start.Add(31 * 24 * time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "usage-summary") {
			fmt.Fprint(w, summaryJSON(start, end, 6746, 30000))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewSession(cursor.New("c", 1, cursor.WithBaseURL(srv.URL)), 5)
	snap, err := p.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v, want a partial snapshot rather than an error", err)
	}
	if snap.SpentCents != 6746 {
		t.Errorf("SpentCents = %d, want 6746", snap.SpentCents)
	}
	if len(snap.TopModels) != 0 {
		t.Errorf("TopModels = %v, want empty when events are unavailable", snap.TopModels)
	}
}

// If the summary itself fails there is nothing worth showing, so the error must
// propagate rather than produce an empty-looking but plausible HUD.
func TestSnapshotFailsWhenTheSummaryFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := NewSession(cursor.New("c", 1, cursor.WithBaseURL(srv.URL)), 5)
	if _, err := p.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot() = nil error, want the auth failure to surface")
	}
}

func TestSnapshotAggregatesModels(t *testing.T) {
	start := time.Now().Add(-24 * time.Hour)
	end := start.Add(31 * 24 * time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "usage-summary") {
			fmt.Fprint(w, summaryJSON(start, end, 300, 30000))
			return
		}
		fmt.Fprintf(w, `{"totalUsageEventsCount":3,"usageEventsDisplay":[
			{"timestamp":"%d","model":"expensive","kind":"%s","chargedCents":200},
			{"timestamp":"%d","model":"cheap","kind":"%s","chargedCents":100},
			{"timestamp":"%d","model":"free","kind":"%s","chargedCents":9999}]}`,
			start.Add(time.Hour).UnixMilli(), cursor.KindUsageBased,
			start.Add(2*time.Hour).UnixMilli(), cursor.KindUsageBased,
			start.Add(3*time.Hour).UnixMilli(), cursor.KindIncludedInBusiness)
	}))
	defer srv.Close()

	p := NewSession(cursor.New("c", 1, cursor.WithBaseURL(srv.URL)), 5)
	snap, err := p.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}

	if len(snap.TopModels) != 2 {
		t.Fatalf("TopModels has %d entries, want 2 (the included-in-plan event must not count)",
			len(snap.TopModels))
	}
	if snap.TopModels[0].Model != "expensive" {
		t.Errorf("TopModels[0] = %q, want the costliest first", snap.TopModels[0].Model)
	}
	if snap.ModelSpendCents != 300 {
		t.Errorf("ModelSpendCents = %v, want 300", snap.ModelSpendCents)
	}
}

// --demo must never touch the network.
func TestStaticProviderMakesNoRequests(t *testing.T) {
	d := &Static{Snap: DemoSnapshot(time.Now())}
	snap, err := d.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	if snap.LimitCents == 0 {
		t.Error("demo snapshot has no budget")
	}
}

func TestStaticProviderDrains(t *testing.T) {
	d := &Static{Snap: DemoSnapshot(time.Now()), Drain: 50 * time.Millisecond}

	first, _ := d.Snapshot(context.Background())
	time.Sleep(60 * time.Millisecond)
	last, _ := d.Snapshot(context.Background())

	if !(last.FractionLeft < first.FractionLeft) {
		t.Errorf("gauge did not drain: %.2f then %.2f", first.FractionLeft, last.FractionLeft)
	}
	if last.FractionLeft != 0 {
		t.Errorf("FractionLeft = %v after the drain window, want 0", last.FractionLeft)
	}
}
