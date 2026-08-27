# How burnrate gets its data

Verified against a live account, 2026-08-26. The scripts here reproduce it.

## The official API doesn't work

| surface | result |
| --- | --- |
| `/teams/*` (Admin API) | `401` — needs a team-admin key. The dashboard only lets regular members mint *user* keys. |
| `/v0/usage`, `/v0/spend`, `/v0/limits` | `404` — the user API has no usage data at all. |
| `/v0/me`, `/v0/models`, `/v0/agents` | `200`, but no spend or quota. |

A user key authenticates fine, so this is a permissions wall, not an auth bug.

## What works instead

The dashboard's own backend, on `https://cursor.com`, authenticated with the
`WorkosCursorSessionToken` cookie.

**`Origin` and `Referer` are mandatory** — without them these routes return
`403`. The cookie is stored percent-encoded, so the `<userId>::<jwt>` separator
arrives as `%3A%3A`.

### `GET /api/usage-summary` — the gauge

```json
{
  "billingCycleStart": "2026-08-25T00:14:35.682Z",
  "isUnlimited": false,
  "individualUsage": { "overall": { "used": 6751, "limit": 30000, "remaining": 23249 } }
}
```

All money is **integer cents**. `individualUsage.overall` is you; `teamUsage` is
the whole org and isn't useful for a personal HUD.

Ignore `autoModelSelectedDisplayMessage` — it said "0% used" while usage was at
22%. It tracks something else.

### `POST /api/dashboard/get-filtered-usage-events` — the model breakdown

```json
{ "teamId": <your team id>, "page": 1, "pageSize": 1000 }
```

Already scoped to the caller. `pageSize` accepts 1000, `page` works, sorted
newest first. Use `chargedCents` (model cost + Cursor fee); `usageBasedCosts` is
a pre-rounded display string. Timestamps are epoch millis **as strings**.

### `POST /api/dashboard/get-monthly-billing-cycle` — the cycle end

Returns `startDateEpochMillis` / `endDateEpochMillis` as strings.

## Three traps

Each produces plausible but wrong numbers, so each is pinned by a test.

**1. Events span multiple cycles.** The endpoint returns recent events
regardless of billing period. On a two-day-old cycle only 147 of 1457 belonged
to it — summing everything overstated spend by **2.2×**. Filter
`timestamp >= billingCycleStart`.

**2. `isChargeable` is not the billing filter.** It's `true` for
`USAGE_EVENT_KIND_INCLUDED_IN_BUSINESS` events, which are never billed.
Filtering on it still overstated the total. Filter on
`kind == "USAGE_EVENT_KIND_USAGE_BASED"`.

**3. `usage-summary`'s cycle end is fake.** It returns start + exactly 31 days.
`get-monthly-billing-cycle` returns a real midnight-UTC boundary, and
`get-team-spend.nextCycleStart` agrees with it. Two sources against one
synthesised value — use the billing endpoint. This drives days-remaining and the
run-out projection, so getting it wrong predicts running dry *after* a reset
that already happened.

## Reconciliation

With both event filters applied, the summed total matched the authoritative
`spendCents` to within 0.8% — and that gap was only usage accruing between the
two calls. `burnrate --once --verify` runs this check; drift moving off ~0% is
the earliest signal Cursor changed something.

## Checked and rejected

| endpoint | why not |
| --- | --- |
| `get-hard-limit` | the **team** cap, not yours |
| `get-team-spend` | authoritative but ships the whole member list for one number — verification only |
| `get-monthly-invoice`, `list-team-service-accounts` | `401` even in the browser; genuinely admin-only |
| `get-daily-spend-by-category` | `200` but empty with every body tried |
| `/api/usage?user=<id>` | vestigial `gpt-4` counters, all zeros |

## Model names aren't normalised

Two formats come back mixed — slugs (`gpt-5.6-sol-xhigh`) and display names
(`Cursor Grok 4.6 (Auto Balanced)`), plus a literal `"default"`. Render what the
API returns; Cursor renames models often and a lookup table would go stale.
