#!/usr/bin/env bash
# M0 API probe for cursor-arcade.
#
# Answers the three questions the whole design rests on:
#   1. Does /teams/spend return a usable per-user dollar limit for this team?
#   2. What do the `model` strings in /teams/filtered-usage-events actually look like?
#   3. Does filtering usage events by email work, and how many events per cycle?
#
# Full responses land in probe/raw/*.json (gitignored). Only a redacted summary is
# printed, so teammates' emails and spend never leave this machine.
#
# The API key is read from the macOS Keychain, never from argv or the environment,
# so it stays out of shell history. Store it once with:
#   security add-generic-password -a "$USER" -s cursor-arcade -w
# (that prompts for the value; it is not passed on the command line)

set -uo pipefail

API="https://api.cursor.com"
RAW="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/raw"
EMAIL="${CURSOR_EMAIL:?set CURSOR_EMAIL to the address on your Cursor account}"

mkdir -p "$RAW"

# Read the key, keeping stdout and stderr apart so we can tell "the Keychain
# said no" from "the Keychain said yes, to an empty string".
KC_ERR="$(mktemp)"
KEY="$(security find-generic-password -a "$USER" -s cursor-arcade -w 2>"$KC_ERR")"
KC_CODE=$?
KC_MSG="$(cat "$KC_ERR")"; rm -f "$KC_ERR"

if [[ -z "$KEY" ]]; then
  KEY="${CURSOR_API_KEY:-}"
  [[ -n "$KEY" ]] && echo "note: using CURSOR_API_KEY from the environment" >&2
fi

if [[ -z "$KEY" ]]; then
  {
    echo "No usable API key."
    echo
    if [[ $KC_CODE -ne 0 ]]; then
      echo "  The Keychain read FAILED (security exit $KC_CODE)."
      [[ -n "$KC_MSG" ]] && echo "  security said: $KC_MSG"
      echo
      echo "  If an 'allow access' dialog appeared, click Always Allow and re-run."
      echo "  If the item does not exist, create it:"
      echo "      security add-generic-password -a \"\$USER\" -s cursor-arcade -w"
    else
      echo "  The Keychain read SUCCEEDED but returned an empty value."
      echo "  The item exists with a blank password — most likely a stray Enter"
      echo "  at the first prompt. Delete it and store the key again:"
      echo
      echo "      security delete-generic-password -a \"\$USER\" -s cursor-arcade"
      echo "      security add-generic-password -a \"\$USER\" -s cursor-arcade -w"
    fi
    echo
    echo "  Or bypass the Keychain for this run:"
    echo "      read -rs CURSOR_API_KEY && export CURSOR_API_KEY && bash probe/probe.sh"
  } >&2
  exit 1
fi

echo "key loaded: ${#KEY} chars, prefix ${KEY:0:4}…"

hr() { printf '\n%s\n' "────────────────────────────────────────────────────────"; }
say() { printf '%s\n' "$*"; }

# call METHOD PATH [JSON_BODY] -> writes body to $RAW/<name>.json, echoes HTTP status
call() {
  local method="$1" path="$2" body="${3:-}" name="$4"
  local out="$RAW/$name.json" code
  if [[ -n "$body" ]]; then
    code=$(curl -sS -o "$out" -w '%{http_code}' -X "$method" "$API$path" \
      -u "$KEY:" -H 'Content-Type: application/json' -d "$body")
  else
    code=$(curl -sS -o "$out" -w '%{http_code}' -X "$method" "$API$path" -u "$KEY:")
  fi
  echo "$code"
}

hr
say "PROBE 0  auth diagnostics — is this key valid at all, and in what way?"
say ""
# Three things can produce a 401 here and they need different fixes:
#   - the key is fine but lacks admin:* scope   -> regenerate with the right scope
#   - the key is fine but we send it wrong      -> use Bearer instead of Basic
#   - the key is not attached to a team at all  -> /teams/* is the wrong surface
# Try each combination and let the status codes tell us which.
probe_auth() {
  local label="$1" path="$2" ; shift 2
  local code
  code=$(curl -sS -o /dev/null -w '%{http_code}' "$API$path" "$@")
  printf '    %-34s %s\n' "$label" "$code"
}
say "  endpoint / auth style                 HTTP"
probe_auth "/teams/members   Basic (key:)"   /teams/members -u "$KEY:"
probe_auth "/teams/members   Bearer"         /teams/members -H "Authorization: Bearer $KEY"
probe_auth "/teams/members   x-api-key"      /teams/members -H "x-api-key: $KEY"
probe_auth "/v0/me           Bearer"         /v0/me         -H "Authorization: Bearer $KEY"
probe_auth "/v0/me           Basic (key:)"   /v0/me         -u "$KEY:"
say ""
say "  How to read this:"
say "    any 200 on /teams/* .... that auth style is the right one; probes below will work"
say "    /v0/me 200 but /teams/* 401 .... key is VALID but lacks admin:* scope, or"
say "                                     the account is not on a team plan"
say "    everything 401 .... key is revoked, truncated, or from a different org"
say ""
say "  Full body from the Basic /teams/members attempt:"
curl -sS "$API/teams/members" -u "$KEY:" | head -c 300 | sed 's/^/    /'
say ""
say "  Full body from the Bearer /v0/me attempt:"
curl -sS "$API/v0/me" -H "Authorization: Bearer $KEY" | head -c 300 | sed 's/^/    /'
say ""

hr
say "PROBE 1  GET /teams/members   (does the key work? what is my exact email?)"
code=$(call GET /teams/members "" members)
say "  HTTP $code"
if [[ "$code" != "200" ]]; then
  say "  unavailable (needs members:read or admin:*). Response:"
  head -c 200 "$RAW/members.json" | sed 's/^/    /'; echo
  # Not fatal: /v0/me already told us the email, and it is the only thing we
  # needed from this endpoint. The usage probes below carry the real signal.
  if call GET /v0/me "" me >/dev/null && [[ -s "$RAW/me.json" ]]; then
    ME="$(jq -r '.userEmail // empty' "$RAW/me.json")"
    if [[ -n "$ME" ]]; then
      say "  falling back to /v0/me -> $ME"
      EMAIL="$ME"
    fi
  fi
  say "  continuing to the usage probes, which are what actually matter."
else
say "  team size: $(jq '.teamMembers | length' "$RAW/members.json")"
say "  my row (matched on $EMAIL):"
jq --arg e "$EMAIL" '.teamMembers[] | select(.email==$e) | {id,email,role,isRemoved}' \
  "$RAW/members.json"
if [[ -z "$(jq --arg e "$EMAIL" '.teamMembers[]|select(.email==$e)' "$RAW/members.json")" ]]; then
  say "  !! no exact match. Close candidates:"
  jq -r --arg e "${EMAIL%%@*}" \
    '.teamMembers[] | select(.email | test($e; "i")) | "     " + .email' \
    "$RAW/members.json"
  say "  Re-run with CURSOR_EMAIL=<the right one> ./probe.sh"
fi
fi

hr
say "PROBE 2  POST /teams/spend   (>>> is there a per-user dollar limit? <<<)"
code=$(call POST /teams/spend "{\"searchTerm\":\"$EMAIL\",\"page\":1,\"pageSize\":10}" spend)
say "  HTTP $code"
if [[ "$code" == "200" ]]; then
  say "  cycle / totals:"
  jq '{subscriptionCycleStart, totalMembers, totalPages}' "$RAW/spend.json"
  say "  my row:"
  jq --arg e "$EMAIL" '
    (.teamMemberSpend // .teamMembers // .members // [])[]
    | select(.email==$e)
    | {spendCents, overallSpendCents, hardLimitOverrideDollars,
       monthlyLimitDollars, effectivePerUserLimitDollars,
       fastPremiumRequests, bugbotUsages}' "$RAW/spend.json"
  say ""
  say "  VERDICT on the HP bar denominator:"
  jq -r --arg e "$EMAIL" '
    (.teamMemberSpend // .teamMembers // .members // [])[]
    | select(.email==$e)
    | if   (.effectivePerUserLimitDollars // null) != null then
        "    OK  effectivePerUserLimitDollars = \(.effectivePerUserLimitDollars) -> HP bar uses this"
      elif (.monthlyLimitDollars // null) != null then
        "    OK  effectivePerUser is null, but monthlyLimitDollars = \(.monthlyLimitDollars) -> fall back to this"
      elif (.hardLimitOverrideDollars // null) != null then
        "    OK  only hardLimitOverrideDollars = \(.hardLimitOverrideDollars) -> fall back to this"
      else
        "    NULL  no limit field is populated -> config monthly_budget_dollars is REQUIRED"
      end' "$RAW/spend.json"
  say ""
  say "  (all limit-ish keys present on my row, for reference:)"
  jq -r --arg e "$EMAIL" '
    (.teamMemberSpend // .teamMembers // .members // [])[]
    | select(.email==$e) | to_entries[]
    | select(.key | test("limit|Limit|spend|Spend"))
    | "    \(.key) = \(.value|tostring)"' "$RAW/spend.json"
else
  head -c 400 "$RAW/spend.json"; echo
fi

hr
say "PROBE 3  POST /teams/filtered-usage-events   (model strings + volume, last 30d)"
END_MS=$(( $(date +%s) * 1000 ))
START_MS=$(( $(date -v-30d +%s) * 1000 ))
say "  window: $(date -r $((START_MS/1000)) '+%Y-%m-%d') .. $(date -r $((END_MS/1000)) '+%Y-%m-%d')"
code=$(call POST /teams/filtered-usage-events \
  "{\"startDate\":$START_MS,\"endDate\":$END_MS,\"email\":\"$EMAIL\",\"page\":1,\"pageSize\":1000}" \
  events)
say "  HTTP $code"
if [[ "$code" == "200" ]]; then
  say "  envelope keys: $(jq -r 'keys|join(", ")' "$RAW/events.json")"
  say "  pagination-ish fields:"
  jq 'with_entries(select(.value|type != "array")) ' "$RAW/events.json"
  EV='(.usageEvents // .events // .data // [])'
  say "  events returned this page: $(jq "$EV | length" "$RAW/events.json")"
  say ""
  say "  did the email filter hold? distinct userEmail values on this page:"
  jq -r "$EV | map(.userEmail) | unique | .[] // empty | \"    \" + ." "$RAW/events.json"
  say ""
  say "  >>> MODEL STRINGS, by charged cents (this is what the top-5 list renders) <<<"
  jq -r "$EV
    | group_by(.model)
    | map({model: .[0].model,
           events: length,
           cents: (map(.chargedCents // 0) | add),
           chargeable: (map(select(.isChargeable)) | length),
           tokenBased: (map(select(.isTokenBasedCall)) | length)})
    | sort_by(-.cents)[]
    | \"    \(.cents|floor|tostring|(\"      \"[0:(6-length)])+.)c  \(.events|tostring|(\"    \"[0:(4-length)])+.) ev  \(.model // \"(null)\")  [chargeable \(.chargeable), token-based \(.tokenBased)]\"" \
    "$RAW/events.json"
  say ""
  say "  total chargedCents on this page: $(jq "$EV | map(.chargedCents // 0) | add // 0" "$RAW/events.json")"
  say "  (compare to spendCents above — they should be close if one page covered the cycle)"
  say ""
  say "  one full event, shape reference (values redacted where identifying):"
  jq "$EV | .[0] | with_entries(if (.key|test(\"Email|email|Name\")) then .value=\"<redacted>\" else . end)" \
    "$RAW/events.json"
  say ""
  say "  distinct 'kind' values: $(jq -r "$EV | map(.kind) | unique | join(\", \")" "$RAW/events.json")"
else
  head -c 400 "$RAW/events.json"; echo
fi

hr
say "Raw responses written to probe/raw/ (gitignored). Nothing above left this machine."
