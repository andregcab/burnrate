#!/usr/bin/env bash
# Endpoint discovery for cursor-arcade.
#
# The team admin key turned out to be unobtainable (the dashboard only mints
# user keys). Before falling back to anything unofficial, find out exactly how
# far the user key we DO have can reach.
#
# Every request is a plain read. Nothing here writes, and the key is never printed.

set -uo pipefail

RAW="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/raw"
mkdir -p "$RAW"

KEY="$(security find-generic-password -a "$USER" -s cursor-arcade -w 2>/dev/null)"
[[ -z "$KEY" ]] && KEY="${CURSOR_API_KEY:-}"
if [[ -z "$KEY" ]]; then
  echo "No API key. See probe.sh for how to store one." >&2
  exit 1
fi
echo "key loaded: ${#KEY} chars, prefix ${KEY:0:4}…"

# try METHOD HOST PATH [BODY]
# Prints one aligned row: status, size, and a snippet of the body for anything
# that came back 200 (so we can see whether it is actually useful).
try() {
  local method="$1" host="$2" path="$3" body="${4:-}"
  local tmp code size snippet slug
  tmp="$(mktemp)"
  slug="$(echo "$method$host$path" | tr -c 'A-Za-z0-9' '-' | cut -c1-60)"

  if [[ -n "$body" ]]; then
    code=$(curl -sS -m 15 -o "$tmp" -w '%{http_code}' -X "$method" "https://$host$path" \
      -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' -d "$body" 2>/dev/null)
  else
    code=$(curl -sS -m 15 -o "$tmp" -w '%{http_code}' -X "$method" "https://$host$path" \
      -H "Authorization: Bearer $KEY" 2>/dev/null)
  fi

  size=$(wc -c <"$tmp" | tr -d ' ')

  if [[ "$code" == "200" && "$size" -gt 2 ]]; then
    cp "$tmp" "$RAW/found-$slug.json"
    snippet="$(head -c 160 "$tmp" | tr -d '\n')"
    printf '  \033[1m%-3s %-52s %6s  %s\033[0m\n' "$code" "$path" "$size" "$snippet"
  else
    snippet="$(head -c 60 "$tmp" | tr -d '\n')"
    printf '  %-3s %-52s %6s  %s\n' "$code" "$path" "$size" "$snippet"
  fi
  rm -f "$tmp"
}

NOW_MS=$(( $(date +%s) * 1000 ))
AGO_MS=$(( $(date -v-30d +%s) * 1000 ))
RANGE="{\"startDate\":$AGO_MS,\"endDate\":$NOW_MS}"

echo
echo "════ api.cursor.com — documented user surface (/v0) ════"
for p in /v0/me /v0/usage /v0/usage/summary /v0/spend /v0/limits /v0/quota \
         /v0/models /v0/agents /v0/me/usage /v0/me/spend /v0/subscription; do
  try GET api.cursor.com "$p"
done

echo
echo "════ api.cursor.com — team surface (expected 401, confirming the wall) ════"
try POST api.cursor.com /teams/spend '{"page":1,"pageSize":5}'
try POST api.cursor.com /teams/filtered-usage-events "$RANGE"
try POST api.cursor.com /teams/daily-usage-data "$RANGE"
try GET  api.cursor.com /teams/me

echo
echo "════ api.cursor.com — organization surface ════"
try GET  api.cursor.com /organizations/spend
try GET  api.cursor.com /organizations/pooled-usage
try GET  api.cursor.com /organizations/members

echo
echo "════ cursor.com/api — what the dashboard itself calls ════"
# These normally authenticate with a session cookie rather than an API key.
# If any accept the Bearer key, that is the cleanest possible fallback.
for p in /api/usage /api/auth/me /api/dashboard/get-monthly-invoice \
         /api/dashboard/get-hard-limit /api/dashboard/get-user-usage \
         /api/usage-summary /api/me; do
  try GET cursor.com "$p"
done

echo
echo "Bold rows are 200s with a real body — anything there is usable."
echo "Full bodies for those saved to probe/raw/found-*.json (gitignored)."
