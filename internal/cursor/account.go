package cursor

import (
	"context"
	"net/http"
)

// Account is the subset of /api/auth/stripe we care about. The endpoint is
// named for billing but is the only place that reports the team id, which the
// usage-events endpoint requires. Discovering it here means `init` can set
// itself up without the user hunting through dashboard URLs.
type Account struct {
	MembershipType           string `json:"membershipType"`
	TeamID                   int    `json:"teamId"`
	IsTeamMember             bool   `json:"isTeamMember"`
	TeamMembershipType       string `json:"teamMembershipType"`
	IndividualMembershipType string `json:"individualMembershipType"`
}

// Account fetches account and team identity.
func (c *Client) Account(ctx context.Context) (*Account, error) {
	var a Account
	if err := c.do(ctx, http.MethodGet, "/api/auth/stripe", nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}
