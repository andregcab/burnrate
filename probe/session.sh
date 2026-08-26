#!/usr/bin/env bash
# Session-cookie probe for cursor-arcade.
#
# The Admin API is closed to non-admins and the /v0 user API has no usage data,
# so the dashboard's own backend is the remaining source. It authenticates with
# the WorkosCursorSessionToken cookie rather than an API key.
#
# This maps the response shapes we need before writing any Go:
#   - what the hard limit looks like  (HP bar denominator)
#   - what spend-so-far looks like    (HP bar numerator)
#   - what per-model usage looks like (top-5 list)
#
# Everything here is a read of your own account. Full bodies land in
# probe/raw/session-*.json (gitignored); only shapes and your own totals print.

set -uo pipefail

RAW="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/raw"
mkdir -p "$RAW"

COOKIE="$(security find-generic-password -a "$USER" -s cursor-arcade-cookie -w 2>/dev/null)"
[[ -z "$COOKIE" ]] && COOKIE="${CURSOR_SESSION_COOKIE:-}"
if [[ -z "$COOKIE" ]]; then
  cat >&2 <<'EOF'
No session cookie found.

Get it from your browser:
  1. Open https://cursor.com/dashboard while logged in
  2. DevTools (Cmd-Opt-I) -> Application -> Cookies -> https://cursor.com
  3. Copy the FULL value of  WorkosCursorSessionToken
  4. Copy it to your clipboard, then run:

     security add-generic-password -a "$USER" -s cursor-arcade-cookie -w "$(pbpaste)" -U

Then re-run this script.
EOF
  exit 1
fi

# The cookie is "<userId>::<jwt>", but the browser stores it percent-encoded, so
# the separator arrives as %3A%3A. Decode before splitting.
DECODED="${COOKIE//%3A/:}"
USER_ID="${DECODED%%::*}"
echo "cookie loaded: ${#COOKIE} chars"
echo "user id:       $USER_ID"
[[ "$USER_ID" == "$DECODED" ]] && echo "  (warning: no '::' separator found — cookie may be truncated)"

# Discovered from the dashboard's own usage-summary?teamId=... request.
TEAM_ID="${CURSOR_TEAM_ID:-1234567}"
echo "team id:       $TEAM_ID"

UA='Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36'

# hit NAME METHOD PATH [BODY]
hit() {
  local name="$1" method="$2" path="$3" body="${4:-}"
  local out="$RAW/session-$name.json" code
  # Origin and Referer are the difference between 403 and 200 here: these are
  # Next.js routes with CSRF checks, and the browser always sends them.
  local hdrs=(
    -H "Cookie: WorkosCursorSessionToken=$COOKIE"
    -H "User-Agent: $UA"
    -H 'Accept: */*'
    -H 'Origin: https://cursor.com'
    -H 'Referer: https://cursor.com/dashboard'
    -H 'Sec-Fetch-Site: same-origin'
    -H 'Sec-Fetch-Mode: cors'
    -H 'Sec-Fetch-Dest: empty'
  )
  if [[ -n "$body" ]]; then
    code=$(curl -sS -m 20 -o "$out" -w '%{http_code}' -X "$method" "https://cursor.com$path" \
      "${hdrs[@]}" -H 'Content-Type: application/json' -d "$body" 2>/dev/null)
  else
    code=$(curl -sS -m 20 -o "$out" -w '%{http_code}' -X "$method" "https://cursor.com$path" \
      "${hdrs[@]}" 2>/dev/null)
  fi

  local size; size=$(wc -c <"$out" | tr -d ' ')
  # A 200 full of HTML is the SPA catch-all, i.e. the route does not exist.
  if [[ "$code" == "200" ]] && head -c 20 "$out" | grep -qi '<!doctype\|<html'; then
    printf '  %-3s %-46s %8s  (HTML — route does not exist)\n' "$code" "$path" "$size"
    rm -f "$out"; return 1
  fi
  printf '  %-3s %-46s %8s\n' "$code" "$path" "$size"
  [[ "$code" == "200" && "$size" -gt 2 ]]
}

MONTH=$(date +%-m); YEAR=$(date +%Y)

START_ISO="$(date -u -v-30d '+%Y-%m-%dT%H:%M:%S.000Z')"
END_ISO="$(date -u '+%Y-%m-%dT%H:%M:%S.000Z')"
START_MS=$(( $(date -v-30d +%s) * 1000 ))
END_MS=$(( $(date +%s) * 1000 ))

echo
echo "════════ endpoint sweep (real names, from the Network tab) ════════"
hit usage-summary      GET  "/api/usage-summary"                        && OK_SUMMARY=1
hit usage-summary-team GET  "/api/usage-summary?teamId=$TEAM_ID"        && OK_SUMMARY_TEAM=1
hit stripe             GET  "/api/auth/stripe"                          && OK_STRIPE=1
hit me                 GET  "/api/dashboard/me"
hit plan-info          GET  "/api/dashboard/get-plan-info"
hit billing-cycle      POST "/api/dashboard/get-monthly-billing-cycle" \
  "{\"teamId\":$TEAM_ID}"                                               && OK_CYCLE=1
hit hard-limit         POST "/api/dashboard/get-hard-limit" '{}'        && OK_LIMIT=1

# The model breakdown. Body shape is a guess; if it 400s, the Payload tab in
# DevTools has the exact one.
hit filt-events        POST "/api/dashboard/get-filtered-usage-events" \
  "{\"teamId\":$TEAM_ID,\"startDate\":\"$START_ISO\",\"endDate\":\"$END_ISO\",\"page\":1,\"pageSize\":100}" \
  && OK_EVENTS=1
hit filt-events-ms     POST "/api/dashboard/get-filtered-usage-events" \
  "{\"teamId\":$TEAM_ID,\"startDate\":$START_MS,\"endDate\":$END_MS,\"page\":1,\"pageSize\":100}" \
  && OK_EVENTS_MS=1
hit filt-events-bare   POST "/api/dashboard/get-filtered-usage-events" \
  "{\"teamId\":$TEAM_ID}"                                               && OK_EVENTS_BARE=1

hit daily-category     POST "/api/dashboard/get-daily-spend-by-category" \
  "{\"teamId\":$TEAM_ID,\"startDate\":\"$START_ISO\",\"endDate\":\"$END_ISO\"}" \
  && OK_CATEGORY=1
hit team-spend         POST "/api/dashboard/get-team-spend" \
  "{\"teamId\":$TEAM_ID}"                                               && OK_TEAMSPEND=1

echo
echo "════════ HP BAR — limit and usage ════════"
[[ -n "${OK_LIMIT:-}" ]]   && { echo "  get-hard-limit:"; jq '.' "$RAW/session-hard-limit.json"; }
[[ -n "${OK_SUMMARY:-}" ]] && { echo "  usage-summary (personal):"; jq '.' "$RAW/session-usage-summary.json"; }
[[ -n "${OK_CYCLE:-}" ]]   && { echo "  get-monthly-billing-cycle:"; jq '.' "$RAW/session-billing-cycle.json"; }

echo
echo "════════ >>> TOP MODELS — get-filtered-usage-events <<< ════════"
# Whichever body shape worked, describe it. This is requirement 5.
for v in filt-events filt-events-ms filt-events-bare; do
  f="$RAW/session-$v.json"
  [[ -s "$f" ]] || continue
  echo
  echo "  ── variant: $v"
  echo "  envelope keys: $(jq -r 'if type=="object" then (keys|join(", ")) else type end' "$f")"

  # Find the array of events wherever it lives.
  echo "  array fields and their lengths:"
  jq -r 'if type=="object"
         then (to_entries[] | select(.value|type=="array") | "    \(.key): \(.value|length)")
         else "    (top level is a \(type))" end' "$f"

  echo "  scalar fields:"
  jq -r 'if type=="object"
         then (to_entries[] | select(.value|type|IN("number","string","boolean"))
               | "    \(.key) = \(.value|tostring)")
         else empty end' "$f" | head -15

  EV='(.usageEvents // .usageEventsDisplay // .events // .data // .items // [])'
  n=$(jq "$EV | length" "$f" 2>/dev/null || echo 0)
  if [[ "$n" -gt 0 ]]; then
    echo
    echo "  >>> grouped by model (this is the top-5 list) <<<"
    jq -r "$EV
      | group_by(.model // .details.model // .modelIntent // \"unknown\")
      | map({model: (.[0].model // .[0].details.model // .[0].modelIntent // \"unknown\"),
             events: length,
             cents: (map(.priceCents // .chargedCents // .cents // .costCents // 0) | add)})
      | sort_by(-.cents)[]
      | \"    \(.cents)c  \(.events) ev  \(.model)\"" "$f" 2>/dev/null | head -20

    echo
    echo "  one event, full shape (emails redacted):"
    jq "$EV | .[0]
        | if type==\"object\"
          then with_entries(if (.key|test(\"[Ee]mail|[Nn]ame\")) then .value=\"<redacted>\" else . end)
          else . end" "$f"
  fi
done

echo
echo "════════ get-daily-spend-by-category ════════"
[[ -n "${OK_CATEGORY:-}" ]] && jq '.' "$RAW/session-daily-category.json" | head -60

echo
echo "════════ subscription / plan ════════"
[[ -n "${OK_STRIPE:-}" ]] && jq '.' "$RAW/session-stripe.json"

echo
echo "════════════════════════════════════════"
echo "Full bodies in probe/raw/session-*.json (gitignored)."
echo "The cookie was never printed."
