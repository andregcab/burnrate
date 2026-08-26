# M0 findings — the Cursor data contract

Everything below was verified against a live account on 2026-08-26. The probe
scripts in this directory reproduce it.

## The short version

The **official Admin API is unusable** for this tool unless you are a Cursor team
admin. The **dashboard's own backend**, authenticated with a browser session
cookie, gives us everything we need.

## Why not the official API

| Surface | Result |
| --- | --- |
| `/teams/*` (Admin API) | `401 Invalid Team API Key` — needs a team admin key. The dashboard only mints **user** keys (`cursor.com/dashboard/api?section=user-keys`), and there is no team-key section for non-admins. |
| `/v0/usage`, `/v0/spend`, `/v0/limits`, `/v0/quota`, `/v0/me/usage` | `404` — these routes do not exist. The user API has no usage data at all. |
| `/v0/me`, `/v0/models`, `/v0/agents` | `200`, but none carry spend or quota. |
| `/organizations/*` | `401` / `404`. |

A user API key (`crsr_` + 64 chars) authenticates fine — `/v0/me` returns 200 —
so this is a permissions and surface-area wall, not an auth bug.

## What we use instead

Base `https://cursor.com`. Auth is the `WorkosCursorSessionToken` cookie.

**`Origin` and `Referer` headers are mandatory.** Without them these routes
return `403`; with them, `200`. They are Next.js routes with CSRF checks.

```
Cookie: WorkosCursorSessionToken=<token>
Origin: https://cursor.com
Referer: https://cursor.com/dashboard
Content-Type: application/json      # on POSTs
```

The cookie is stored percent-encoded: the `<userId>::<jwt>` separator arrives as
`%3A%3A` and must be decoded before splitting.

### `GET /api/usage-summary` — the HP bar

506 bytes, one request, everything for requirements 3 and 4.

```json
{
  "billingCycleStart": "2026-08-25T00:14:35.682Z",
  "billingCycleEnd":   "2026-09-25T00:14:35.682Z",
  "membershipType": "enterprise",
  "limitType": "team",
  "isUnlimited": false,
  "individualUsage": { "overall": {
      "enabled": true, "used": 6751, "limit": 30000, "remaining": 23249 } },
  "teamUsage": { "onDemand": {
      "enabled": true, "used": <team total>, "limit": <team cap>, "remaining": <team remaining> } }
}
```

**All money is integer cents.** `individualUsage.overall` is you; `teamUsage` is
the whole org and is not useful for a personal HUD.

Confirmed against `get-team-spend`, which reported the same person with a
matching `spendCents` and an `effectivePerUserLimitDollars` equal to
`individualUsage.overall.limit / 100`. `teamUsage.onDemand.limit` likewise
matched the team cap shown in dashboard settings, which is how the cents unit
was established.

Ignore `autoModelSelectedDisplayMessage` / `namedModelSelectedDisplayMessage`.
They said "You've used 0%" while usage was at 22.5% — they track a different
metric and would mislead.

### `POST /api/dashboard/get-filtered-usage-events` — the model breakdown

```json
{ "teamId": <your team id>, "page": 1, "pageSize": 1000 }
```

Returns `{ "totalUsageEventsCount": 1457, "usageEventsDisplay": [...] }`.

- **Already scoped to the caller.** All 1,457 events carried a single
  `owningUser`. No user filter is needed or available.
- **`pageSize` accepts 1000** (verified 100 / 500 / 1000). A cycle is one or two
  requests, not fifteen.
- **`page` works correctly** — distinct time ranges, zero duplicates across all
  15 pages of a 1,457-row set.
- Sorted newest first.

Event shape:

```json
{
  "timestamp": "1787773712502",          // epoch millis AS A STRING
  "model": "gpt-5.6-sol-xhigh",
  "kind": "USAGE_EVENT_KIND_USAGE_BASED",
  "isTokenBasedCall": true,
  "tokenUsage": { "inputTokens": 6, "outputTokens": 298,
                  "cacheWriteTokens": 526, "cacheReadTokens": 55274,
                  "totalCents": 3.0723 },
  "cursorTokenFee": 1.4026,
  "chargedCents": 4.4749,                // totalCents + cursorTokenFee
  "isChargeable": true,
  "isHeadless": true,                    // true for cloud agents
  "owningUser": "<your numeric user id>",
  "cloudAgentId": "...", "conversationId": "..."
}
```

Use `chargedCents` — it is the model cost plus the Cursor fee, and it is what
reconciles. `usageBasedCosts` is a pre-rounded display string (`"$0.03"`).

## The two traps

**1. Events span more than one billing cycle.** The endpoint returns recent
events regardless of cycle. On a cycle two days old, only **147 of 1,457** events
belonged to it. Summing the unfiltered list overstates spend by **2.2×**.
Always filter `timestamp >= billingCycleStart`.

**2. `isChargeable` is not the billing filter.** It is `true` for
`USAGE_EVENT_KIND_INCLUDED_IN_BUSINESS` events, which are *not* billed. Filtering
on it still overstated the total (14,806c vs 6,746c). Filter on
`kind == "USAGE_EVENT_KIND_USAGE_BASED"` instead.

Kinds observed: `USAGE_EVENT_KIND_USAGE_BASED` (billed),
`USAGE_EVENT_KIND_INCLUDED_IN_BUSINESS` (covered by the plan),
`USAGE_EVENT_KIND_ERRORED_NOT_CHARGED`.

### Reconciliation

With both filters applied, event sum **6,801.57c** against authoritative
**6,746c** — 0.8% apart, and only because usage accrued between the two calls
(`used` was observed ticking 6746 → 6751 during the probe run). This is the
regression test: aggregate, compare to `usage-summary`, expect agreement within
a small tolerance.

## Model names are not normalized

Two formats come back mixed, sometimes for the same underlying model:

- slugs — `gpt-5.6-sol-xhigh`, `claude-opus-5-thinking-high`, `composer-2.5`
- display names — `Cursor Grok 4.6 (Auto Balanced)`, `Claude Opus 5 (Auto Balanced)`

Also a literal `"default"` (56 events). Render whatever the API returns rather
than mapping to a fixed set; Cursor renames models frequently.

## Endpoints checked and rejected

| Endpoint | Result |
| --- | --- |
| `get-hard-limit` | Returns the **team** cap in dollars, not your personal one. |
| `get-team-spend` | Works and is authoritative, but ships the entire member list to tell you one number. Verification only, never the hot path. |
| `get-monthly-billing-cycle` | Returns a period that disagrees with `usage-summary`. Prefer `usage-summary`. |
| `get-monthly-invoice`, `list-team-service-accounts` | `401` even in the browser — genuinely admin-only. |
| `get-daily-spend-by-category` | `200` but empty with every body tried. |
| `/api/usage?user=<id>` | Vestigial `gpt-4` counters, all zeros. |

## Consequences for the build

- Poll `usage-summary` on the refresh interval — 506 bytes, cheap.
- Fetch events with `pageSize: 1000`, page until `timestamp < billingCycleStart`,
  then stop. Early in a cycle that is one request.
- Cache a per-model rollup keyed by `billingCycleStart`; wipe when it changes.
- The session cookie expires. `init` must detect a 401 and say so plainly rather
  than render stale numbers as current.
