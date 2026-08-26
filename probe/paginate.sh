#!/usr/bin/env bash
# Final M0 question: can we aggregate an accurate per-model breakdown?
#
# Page 1 of get-filtered-usage-events summed to ~$51 over 25 hours, but the whole
# cycle is $67.46 across 1456 events. Either the page is sorted by cost, or the
# event count includes things that do not contribute to spend. A top-5 built on a
# skewed page would be quietly wrong, so walk every page and reconcile the total
# against the authoritative spendCents before trusting any of it.

set -uo pipefail

RAW="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/raw"
mkdir -p "$RAW"

COOKIE="$(security find-generic-password -a "$USER" -s cursor-arcade-cookie -w 2>/dev/null)"
[[ -z "$COOKIE" ]] && COOKIE="${CURSOR_SESSION_COOKIE:-}"
[[ -z "$COOKIE" ]] && { echo "No session cookie. See session.sh." >&2; exit 1; }

TEAM_ID="${CURSOR_TEAM_ID:?set CURSOR_TEAM_ID (see teamId in GET /api/auth/stripe)}"
UA='Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36'
EP="https://cursor.com/api/dashboard/get-filtered-usage-events"

post() { # post BODY OUTFILE
  curl -sS -m 30 -o "$2" -X POST "$EP" \
    -H "Cookie: WorkosCursorSessionToken=$COOKIE" -H "User-Agent: $UA" \
    -H 'Content-Type: application/json' -H 'Accept: */*' \
    -H 'Origin: https://cursor.com' -H 'Referer: https://cursor.com/dashboard' \
    -d "$1" 2>/dev/null
}

echo "════ does pageSize go above 100? ════"
for size in 100 500 1000; do
  post "{\"teamId\":$TEAM_ID,\"page\":1,\"pageSize\":$size}" "$RAW/pg-size-$size.json"
  n=$(jq -r '.usageEventsDisplay|length // 0' "$RAW/pg-size-$size.json" 2>/dev/null || echo err)
  tot=$(jq -r '.totalUsageEventsCount // "?"' "$RAW/pg-size-$size.json" 2>/dev/null)
  echo "  pageSize=$size -> returned $n events (total reported: $tot)"
done

echo
echo "════ does the page param advance? ════"
for p in 1 2 3; do
  post "{\"teamId\":$TEAM_ID,\"page\":$p,\"pageSize\":100}" "$RAW/pg-$p.json"
  jq -r --arg p "$p" '.usageEventsDisplay
    | "  page \($p): \(length) events, newest \(([.[].timestamp|tonumber]|max)/1000|todate), oldest \(([.[].timestamp|tonumber]|min)/1000|todate), sum \((map(.chargedCents//0)|add)|.*100|round/100)c"' \
    "$RAW/pg-$p.json" 2>/dev/null || echo "  page $p: unreadable"
done
echo "  (if pages 1-3 show identical timestamps, the page param is ignored)"

echo
echo "════ walking every page ════"
PAGE=1; MAX=40
: >"$RAW/all-events.jsonl"
while (( PAGE <= MAX )); do
  post "{\"teamId\":$TEAM_ID,\"page\":$PAGE,\"pageSize\":100}" "$RAW/pg-cur.json"
  n=$(jq -r '.usageEventsDisplay|length // 0' "$RAW/pg-cur.json" 2>/dev/null || echo 0)
  [[ "$n" == "0" ]] && break
  jq -c '.usageEventsDisplay[]' "$RAW/pg-cur.json" >>"$RAW/all-events.jsonl"
  printf '  page %-3d %s events\n' "$PAGE" "$n"
  (( n < 100 )) && break
  PAGE=$(( PAGE + 1 ))
done

# Pages may repeat if the param is ignored; dedupe on the natural key.
sort -u "$RAW/all-events.jsonl" >"$RAW/all-events-uniq.jsonl"
RAWN=$(wc -l <"$RAW/all-events.jsonl" | tr -d ' ')
UNIQ=$(wc -l <"$RAW/all-events-uniq.jsonl" | tr -d ' ')
echo "  fetched $RAWN rows, $UNIQ unique"
[[ "$RAWN" != "$UNIQ" ]] && echo "  !! duplicates found — the page param is likely ignored"

echo
echo "════ RECONCILIATION ════"
SUM=$(jq -s 'map(.chargedCents//0)|add' "$RAW/all-events-uniq.jsonl")
echo "  sum(chargedCents) over unique events : $(printf '%.2f' "$SUM")c"
MY_ID="$(jq -r '[.[].owningUser] | unique | .[0] // empty' "$RAW/all-events-uniq.jsonl" 2>/dev/null)"
echo "  authoritative spendCents             : $(jq -r --arg id "$MY_ID" '(.teamMemberSpend[]|select((.userId|tostring)==$id)|.spendCents) // "?"' "$RAW/session-team-spend.json" 2>/dev/null || echo '?')c"
echo "  usage-summary individualUsage.used   : $(jq -r '.individualUsage.overall.used' "$RAW/session-usage-summary.json" 2>/dev/null || echo '?')c"
echo
echo "  If these agree, aggregation is trustworthy and the top-5 is exact."
echo "  If the event sum is much larger, events include non-billed usage and the"
echo "  top-5 should rank by event COUNT or by chargeable-only cents instead."

echo
echo "════ TOP MODELS over everything fetched ════"
jq -s -r '
  group_by(.model)
  | map({model: .[0].model,
         events: length,
         cents: (map(.chargedCents//0)|add),
         chargeable: (map(select(.isChargeable))|length)})
  | sort_by(-.cents)
  | .[]
  | "  \(((.cents*100)|round)/100 | tostring | (" "*(9-length))+.)c  \((.events|tostring|(" "*(5-length))+.)) ev  \(.model)"
' "$RAW/all-events-uniq.jsonl"

echo
echo "════ same list, ranked by event count ════"
jq -s -r '
  group_by(.model) | map({model: .[0].model, events: length})
  | sort_by(-.events) | .[]
  | "  \((.events|tostring|(" "*(5-length))+.)) ev  \(.model)"
' "$RAW/all-events-uniq.jsonl" | head -10

echo
echo "════ event kinds seen (is anything non-usage-based?) ════"
jq -s -r 'group_by(.kind)|map({kind:.[0].kind,n:length})|.[]|"  \(.n)  \(.kind)"' "$RAW/all-events-uniq.jsonl"
