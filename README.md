# burnrate

Your Cursor spend, at a glance — in the terminal.

```
╔══════════════════════════════════════════════════════════════════════════════╗
║                                                                              ║
║                                                                /\    /\      ║
║    ▂▅▃ BURNRATE                                           ◐   ( ·    · )     ║
║                                                               (   ..   )     ║
║                                                                `------´      ║
╟─ BUDGET ────────────────────────────────────────────────────── 21 DAYS LEFT ─╢
║                                                                              ║
║    CREDITS  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░░░    79%              ║
║                                                                              ║
║    $238.30 left  of $300.00                                  spent $61.70    ║
║                                                                              ║
╟─ MONEY FURNACE ──────────────────────────────────────────────────────────────╢
║                                                                              ║
║    ▛▂▃▂▁▂▜ ╨ $·····$·····$·····$·····$·····$·····$·····$·····$·····$·····    ║
║                                                                              ║
╟─ SPEND BY MODEL ──────────────────────────────────────────────── 167 EVENTS ─╢
║                                                                              ║
║    1. gpt 5.6 sol xhigh           ▓▓▓▓▓▓▓░░░░░░░░░   45.6%         $28.15    ║
║    2. gpt 5.6 luna medium         ▓▓▓▓░░░░░░░░░░░░   25.7%         $15.85    ║
║    3. claude opus 5 high          ▓▓░░░░░░░░░░░░░░   11.5%          $7.08    ║
║                                                                              ║
╟──────────────────────────────────────────────────────────────────────────────╢
║              ◆  $6.86/day   ·   on pace for $212.52 this cycle               ║
╚══════════════════════════════════════════════════════════════════════════════╝
```

Three things at a glance: what's left this billing cycle, the same figure as a
draining gauge, and where the money actually went.

Try it with no setup at all:

```sh
go run github.com/andregcab/burnrate/cmd/burnrate@latest --demo --machine
```

## Install

```sh
brew install andregcab/tap/burnrate     # or: go install github.com/andregcab/burnrate/cmd/burnrate@latest
burnrate init
```

`init` asks for one thing — your Cursor session cookie — then discovers your
team and verifies everything before storing anything.

## Why a browser cookie and not an API key

Cursor's documented Admin API needs a **team-admin** key. The dashboard only
lets ordinary members create *user* keys, and those return `401 Invalid Team API
Key` on every `/teams/*` route. The user-scoped API has no usage data at all —
`/v0/usage`, `/v0/spend`, `/v0/limits` are all `404`.

What does work is the dashboard's own backend, authenticated the same way your
browser authenticates. That's undocumented and could change without notice,
which is why the data source sits behind an interface: if Cursor ships an
accessible API later, it's a new file rather than a rewrite.

The full investigation, including exact response shapes, is in
[`probe/FINDINGS.md`](probe/FINDINGS.md).

**About that cookie:** it is your logged-in session, not a scoped token —
anything you can do on the Cursor dashboard, it can do. burnrate stores it in
your macOS Keychain, never in a file, and only ever reads. Sessions last about
60 days and can't be renewed programmatically, so burnrate reads the expiry from
the cookie itself and starts reminding you a week before it lapses. When it
does, run `burnrate init` again.

## Usage

```
burnrate                    live HUD
burnrate init               setup, or refresh an expired session
burnrate --once             print once and exit
burnrate --json             machine-readable snapshot
burnrate --demo             synthetic data, no credentials
```

In the HUD:

| key | |
|---|---|
| `b` | cycle companion (10 of them) |
| `m` | cycle machine |
| `a` | machine on/off |
| `s` | save the current look as your default |
| `r` | refresh now |
| `l` | toggle the legend |
| `q` | quit |

`--machine=money-furnace` (or `token-factory`, `reactor-core`, `pumpjack`) and
`--buddy duck` pick them from the command line. `s` writes both to
`~/.burnrate/config.toml`.

## Checking it's telling the truth

```sh
burnrate --once --verify
```

This sums your individual usage events and compares the total against the
authoritative figure Cursor reports:

```
  summed from events : $69.00
  authoritative      : $69.00
  drift              : +0.00%  (OK — within tolerance)
```

Worth running every few weeks. Because the data source is undocumented, drift
moving off ~0% is the earliest signal that Cursor changed something — better to
catch it there than to quietly read wrong numbers.

## What it does not do

- **Guess what "high usage" means.** Earlier versions labelled your burn rate
  `HEAVY` or `MAX BURN`. Those thresholds were invented — one person's busy day
  is another's quiet one — so the labels are gone. The machine still speeds up
  with spend, which only ever implies "more than a moment ago", and that much is
  true regardless of baseline.
- **Show team data.** Everything is scoped to you.
- **Write anything.** Every API call is a read.

## Configuration

`~/.burnrate/config.toml`, written by `init` and by the `s` key:

```toml
email = "you@example.com"
team_id = 1234567          # discovered by `init`
refresh_seconds = 300
buddy = "chonk"
machine = "money-furnace"
machine_on = true
# monthly_budget_dollars = 300   # only if the API reports no limit
```

The session cookie is **not** here — it's in the Keychain.

## Development

```sh
go test ./...              # all green, ~75-98% on the packages that matter
go run ./cmd/burnrate --demo --machine
```

Layout:

```
cmd/burnrate      CLI, flags, init wizard
internal/cursor   HTTP client for the dashboard backend
internal/provider Provider interface: session-backed, cached, and demo
internal/stats    Snapshot — the one type every renderer consumes
internal/store    last-good snapshot, for instant start and offline
internal/ui       Bubble Tea HUD: gauges, machines, companions
probe/            the API investigation that made this possible
```

`stats.Snapshot` is the seam. The UI never touches HTTP and the client never
formats anything, so a new front-end needs no changes below it.

Two bugs in here would produce plausible-but-wrong numbers, and both are pinned
by tests rather than comments: usage events span **multiple billing cycles**
(summing them all overstated a real cycle by 2.2×), and `isChargeable` is `true`
for usage that is never billed (filtering on it overstated by another 2×). See
`probe/FINDINGS.md`.

Companion sprites come from the Claude Code buddy set
([gist](https://gist.github.com/zmxv/7f83671f860c15be02f45b07fee207fc)); the
table is generated into `internal/ui/sprites_gen.go`.

## License

MIT — see [LICENSE](LICENSE).
