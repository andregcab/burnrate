// Package cursor talks to the Cursor dashboard's backend.
//
// This is not the documented Admin API. That surface requires a team admin key,
// which ordinary team members cannot create, and the documented user API has no
// usage data at all (see probe/FINDINGS.md). What remains is the same backend
// the dashboard itself calls, authenticated with a browser session cookie.
//
// That means it is undocumented and can change without notice. Everything here
// is a read of your own account; nothing writes.
package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// BaseURL is the dashboard origin. The API lives on the same host.
	BaseURL = "https://cursor.com"

	// MaxPageSize is the largest page get-filtered-usage-events honors.
	// Verified at 100, 500, and 1000; 1000 works.
	MaxPageSize = 1000

	// browserUA matters: these routes are meant for a browser, and sending a
	// plausible agent avoids being singled out as a bot.
	browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
)

// ErrUnauthorized means the session cookie is missing, expired, or rejected.
// Callers should surface this as "log in again", never as a transient failure —
// showing a stale number as if it were current is worse than showing an error.
var ErrUnauthorized = errors.New("session cookie rejected: log in to cursor.com and re-run `burnrate init`")

// ErrForbidden means the request authenticated but was refused. In practice
// this is a missing Origin/Referer header, which these CSRF-checked routes
// require — so it indicates a bug here rather than a problem with the account.
var ErrForbidden = errors.New("request forbidden (CSRF headers missing?)")

// Client reads usage from the Cursor dashboard backend.
type Client struct {
	cookie  string
	teamID  int
	baseURL string
	http    *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client, mainly for tests.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithBaseURL overrides the target host, for tests against an httptest server.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// New builds a Client. The cookie is the raw WorkosCursorSessionToken value,
// percent-encoded exactly as the browser stores it.
func New(cookie string, teamID int, opts ...Option) *Client {
	c := &Client{
		cookie:  cookie,
		teamID:  teamID,
		baseURL: BaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// UserID extracts the user id from the cookie, which has the form
// "<userId>::<jwt>". The browser stores it percent-encoded, so the separator
// arrives as %3A%3A and has to be decoded before splitting.
func UserID(cookie string) string {
	decoded := strings.ReplaceAll(cookie, "%3A", ":")
	if id, _, found := strings.Cut(decoded, "::"); found {
		return id
	}
	return ""
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	// Origin and Referer are load-bearing, not decoration: without them these
	// routes answer 403. Verified in probe/session.sh.
	req.Header.Set("Cookie", "WorkosCursorSessionToken="+c.cookie)
	req.Header.Set("Origin", BaseURL)
	req.Header.Set("Referer", BaseURL+"/dashboard")
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "*/*")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s: %w", path, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	default:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("%s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	// An expired session redirects to the SPA, which answers 200 with HTML.
	// Without this check that would surface as a confusing JSON parse error
	// instead of "your cookie expired".
	if looksLikeHTML(raw) {
		return ErrUnauthorized
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	return nil
}

func looksLikeHTML(b []byte) bool {
	head := strings.ToLower(strings.TrimSpace(string(b[:min(len(b), 64)])))
	return strings.HasPrefix(head, "<!doctype") || strings.HasPrefix(head, "<html")
}

// UsageSummary is GET /api/usage-summary — the whole HP bar in 506 bytes.
func (c *Client) UsageSummary(ctx context.Context) (*Summary, error) {
	var s Summary
	if err := c.do(ctx, http.MethodGet, "/api/usage-summary", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// UsageEventsPage fetches one page of usage events, newest first.
func (c *Client) UsageEventsPage(ctx context.Context, page, pageSize int) (*EventsPage, error) {
	if pageSize <= 0 || pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	if page < 1 {
		page = 1
	}
	body := map[string]any{"teamId": c.teamID, "page": page, "pageSize": pageSize}

	var p EventsPage
	if err := c.do(ctx, http.MethodPost, "/api/dashboard/get-filtered-usage-events", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// UsageEventsSince walks pages newest-first and stops as soon as it sees an
// event older than `since`, returning only events at or after it.
//
// The early exit is the point. The endpoint returns recent events regardless of
// billing cycle — on a two-day-old cycle, 147 of 1457 events belonged to it —
// so walking everything would be both wasteful and, if summed naively, wrong by
// more than 2x.
func (c *Client) UsageEventsSince(ctx context.Context, since time.Time, maxPages int) ([]Event, error) {
	if maxPages <= 0 {
		maxPages = 20
	}
	var out []Event

	for page := 1; page <= maxPages; page++ {
		p, err := c.UsageEventsPage(ctx, page, MaxPageSize)
		if err != nil {
			return out, err
		}
		if len(p.Events) == 0 {
			return out, nil
		}

		for _, ev := range p.Events {
			ts, err := ev.Time()
			if err != nil {
				continue // a malformed timestamp should not poison the run
			}
			if ts.Before(since) {
				return out, nil // newest-first, so everything after is older too
			}
			out = append(out, ev)
		}

		// A short page is the last page.
		if len(p.Events) < MaxPageSize {
			return out, nil
		}
	}
	return out, nil
}

// Summary is the /api/usage-summary response.
type Summary struct {
	BillingCycleStart time.Time `json:"billingCycleStart"`
	BillingCycleEnd   time.Time `json:"billingCycleEnd"`
	MembershipType    string    `json:"membershipType"`
	LimitType         string    `json:"limitType"`
	IsUnlimited       bool      `json:"isUnlimited"`

	IndividualUsage struct {
		Overall Bucket `json:"overall"`
	} `json:"individualUsage"`

	// TeamUsage covers the whole organization. Deliberately unused by the HUD —
	// it is someone else's number.
	TeamUsage struct {
		OnDemand Bucket `json:"onDemand"`
	} `json:"teamUsage"`
}

// Bucket is a used/limit/remaining triple. All values are integer cents.
type Bucket struct {
	Enabled   bool `json:"enabled"`
	Used      int  `json:"used"`
	Limit     int  `json:"limit"`
	Remaining int  `json:"remaining"`
}

// EventsPage is the get-filtered-usage-events response.
type EventsPage struct {
	Total  int     `json:"totalUsageEventsCount"`
	Events []Event `json:"usageEventsDisplay"`
}

// Event is a single usage event. It is already scoped to the authenticated
// user; there is no user filter on the endpoint and none is needed.
type Event struct {
	// Timestamp is epoch milliseconds encoded as a JSON string.
	Timestamp string `json:"timestamp"`

	Model string `json:"model"`
	Kind  string `json:"kind"`

	// ChargedCents is the model cost plus the Cursor token fee, and is the only
	// field that reconciles against the authoritative spend total.
	ChargedCents float64 `json:"chargedCents"`

	// IsChargeable is deliberately not the billing filter: it is true even for
	// INCLUDED_IN_BUSINESS events, which are not billed. Use Kind instead.
	IsChargeable bool `json:"isChargeable"`

	IsHeadless     bool   `json:"isHeadless"`
	OwningUser     string `json:"owningUser"`
	ConversationID string `json:"conversationId"`
}

// Event kinds observed in the wild.
const (
	KindUsageBased         = "USAGE_EVENT_KIND_USAGE_BASED"
	KindIncludedInBusiness = "USAGE_EVENT_KIND_INCLUDED_IN_BUSINESS"
	KindErroredNotCharged  = "USAGE_EVENT_KIND_ERRORED_NOT_CHARGED"
)

// Billed reports whether this event contributes to the spend total.
func (e Event) Billed() bool { return e.Kind == KindUsageBased }

// Time parses the string-encoded epoch-millisecond timestamp.
func (e Event) Time() (time.Time, error) {
	ms, err := strconv.ParseInt(e.Timestamp, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad timestamp %q: %w", e.Timestamp, err)
	}
	return time.UnixMilli(ms), nil
}
