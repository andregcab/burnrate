# burnrate

Your Cursor spend, at a glance.

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

## Install

```sh
brew install andregcab/tap/burnrate
burnrate init
```

`init` asks for one thing: your Cursor session cookie.

1. Open [cursor.com/dashboard](https://cursor.com/dashboard)
2. DevTools (**⌘⌥I**) → Application → Cookies → `cursor.com`
3. Copy the value of `WorkosCursorSessionToken`

Then run `burnrate`.

## Keys

| | | | |
|---|---|---|---|
| `b` | companion | `r` | refresh |
| `m` | machine | `l` | legend |
| `a` | machine on/off | `q` | quit |
| `s` | save this look as your default | | |

Also: `burnrate --once`, `--json`, `--demo`.

## Notes

**The cookie is your logged-in session**, not a scoped token — treat it like a
password. It's stored in your Keychain and only ever read from.

**Sessions last ~60 days.** burnrate warns a week before yours expires; when it
does, run `burnrate init` again.

**Why a cookie and not an API key?** Cursor's Admin API needs a team-admin key
that regular members can't create, and the user API has no usage data.
[`probe/FINDINGS.md`](probe/FINDINGS.md) has the details.

**Check it's accurate** with `burnrate --once --verify` — it reconciles the
per-model totals against the figure Cursor reports.

## Development

```sh
go test ./...
go run ./cmd/burnrate --demo --machine
```

Releasing (local only — CI can't push to the tap without a PAT):

```sh
git tag -a v0.1.2 -m "..." && git push origin v0.1.2
GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

`stats.Snapshot` is the seam: the UI never touches HTTP, the client never
formats. Sprites come from the [Claude Code buddy
set](https://gist.github.com/zmxv/7f83671f860c15be02f45b07fee207fc).

MIT
