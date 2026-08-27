package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/andregcab/burnrate/internal/stats"
)

// Width is the cabinet's total width in cells, borders included. Wide enough
// for a full-length gauge, an unabbreviated model name, and a cost column
// without anything crowding anything else.
const Width = 80

// Layout constants. Content is inset from the walls by gutter cells on each
// side, which is most of what makes the HUD feel unhurried rather than packed.
const (
	gutter = 4

	// popSlotWidth reserves room for the spend pop so it cannot displace the
	// sprite. Sized for the widest realistic pop, e.g. "+$100.00 ✦".
	popSlotWidth = 12

	// gaugeLabel names the main bar. "CREDITS" over "HP" because HP is what you
	// lose to damage, while this is a resource you spend on purpose — and
	// credits is both the arcade word for plays remaining and literally money,
	// which keeps one vocabulary across the gauge, the tokens, and the furnace.
	gaugeLabel = "CREDITS  "

	hpBarWidth    = 44
	modelBarWidth = 16
	nameWidth     = 26
)

// Opts controls how much motion the HUD shows.
type Opts struct {
	// Arcade turns on the refinery. Off by default — the HUD lives in
	// peripheral vision, and motion there is a tax on attention.
	Arcade bool

	// HP is the eased gauge fraction, lagging the true value so the bar glides.
	// Negative means "use the snapshot's own value".
	HP float64

	// CoinAge drives the spend-pop and fires the refinery's pistons; negative
	// means no recent spend.
	CoinAge int

	// CoinCents is the size of the spend that triggered the pop.
	CoinCents float64

	// Machine and Buddy select from the catalogs; both wrap, so the caller can
	// just increment.
	Machine int
	Buddy   int

	// Saved is a transient confirmation shown after the default is stored.
	Saved string

	// Notice is a standing warning, currently only the session cookie nearing
	// expiry. It sits above the footer so it cannot be missed.
	Notice string

	// Legend shows the key bindings for cycling. On by default so the controls
	// are discoverable without a manual.
	Legend bool
}

// popping reports whether a spend registered recently enough to still be shown.
//
// One predicate for both the flourish and the machine's reaction, so they
// cannot end at different times — they previously ran for 4 and 6 frames, and
// the text vanished while the pistons were still going.
func popping(coinAge int) bool { return coinAge >= 0 && coinAge < PopFrames }

// cells is how many of a gauge's cells are filled at a given fraction.
//
// Plain rounding to the nearest whole cell, with one exception: a nonzero
// fraction never rounds down to nothing. A model you actually spent money on
// showing a completely empty bar reads as "unused", which is worse than
// overstating it by a fraction of a cell.
func cells(frac float64, width int) int {
	frac = clamp01(frac)
	n := int(frac*float64(width) + 0.5)
	if n == 0 && frac > 0 {
		n = 1
	}
	if n > width {
		n = width
	}
	return n
}

// Bar renders a proportional gauge, unstyled. Kept for tests and for anything
// that needs the raw glyphs.
func Bar(frac float64, width int) string {
	filled := cells(frac, width)
	return strings.Repeat(blockFull, filled) +
		strings.Repeat(blockEmpty, width-filled)
}

// StyledBar renders a gauge with a shaded empty track.
//
// Both halves are dither patterns, never a full block: █ fills its entire line
// box including leading, so stacked filled runs fuse into one continuous shape
// while the shaded tails beside them keep a clean gap between rows.
func StyledBar(frac float64, width int, fill, track lipgloss.Style) string {
	filled := cells(frac, width)

	var b strings.Builder
	if filled > 0 {
		b.WriteString(fill.Render(strings.Repeat(blockFull, filled)))
	}
	if filled < width {
		b.WriteString(track.Render(strings.Repeat(blockEmpty, width-filled)))
	}
	return b.String()
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// money formats cents the way a person reads them.
func money(cents float64) string { return fmt.Sprintf("$%.2f", cents/100) }

// contentWidth is the usable width between the gutters.
func contentWidth() int { return Width - 2 - gutter*2 }

// Render draws the whole cabinet.
func Render(s stats.Snapshot, now time.Time, frame int, o Opts) string {
	hp := o.HP
	if hp < 0 {
		hp = s.FractionLeft
	}
	low := !s.Unlimited && s.FractionLeft <= lowHP

	var out []string
	row := func(content string) { out = append(out, Row(content, Width, gutter)) }
	blank := func() { out = append(out, Row("", Width, gutter)) }

	out = append(out, Top(Width))
	blank()
	for _, ln := range titleBand(s, frame, low, o) {
		row(ln)
	}

	out = append(out, Divider(Width, "BUDGET", cycleTag(s, now)))
	blank()
	row(gaugeLine(s, hp, low))
	blank()
	row(figuresLine(s))
	blank()

	if o.Arcade {
		// Activity blends the 15-minute burst with the cycle average, so the
		// machine reacts to a burst within a minute or two but does not idle
		// during steady use.
		activity := s.ActivityRate(now, FullScaleCentsPerDay)
		firing := popping(o.CoinAge)

		// No intensity label: the section says which machine is running, and
		// the burn row below states the actual rate. A word like "HEAVY" would
		// be asserting a threshold we have no data for.
		m := MachineAt(o.Machine)
		out = append(out, Divider(Width, m.Name(), ""))
		blank()
		row(m.Row(contentWidth(), frame, activity, firing))
		blank()
	}

	// "SPEND BY MODEL", not "TOP MODELS": the ranking is by dollars, and "top"
	// reads as most-used. The two orders genuinely differ — a model used a
	// hundred times cheaply can rank below one used twice at high effort.
	out = append(out, Divider(Width, "SPEND BY MODEL", eventTag(s)))
	blank()
	for _, ln := range modelLines(s) {
		row(ln)
	}
	blank()

	out = append(out, Divider(Width, "", ""))
	row(center(burnLine(s, now), contentWidth()))

	out = append(out, Divider(Width, "", ""))
	if o.Notice != "" {
		row(styleWarn.Render("▲ " + o.Notice))
	}
	if o.Legend {
		row(legend(o))
	}
	row(footer(s, o))
	out = append(out, Bottom(Width))

	return strings.Join(out, "\n")
}

// titleBand is the top block: the wordmark on the left, the companion on the
// right, each vertically centred in the band.
//
// They sit at opposite ends rather than grouped together, so the band reads as
// a header rather than as a logo. The sprite's art is centred vertically inside
// its own box at load time, which is what lets a single title row line up with
// creatures of very different shapes.
func titleBand(s stats.Snapshot, frame int, low bool, o Opts) []string {
	buddy := BuddyAt(o.Buddy)
	face := buddy.Face(frame, low)

	wordmark := Flame(frame) + " " + styleTitle.Render("BURNRATE")

	// The spinner and any spend pop share one fixed-width slot. Fixed, because
	// a pop like "+$10.04 ✦" is far wider than "◐" and would otherwise shove
	// the sprite sideways every time spend landed.
	slot := styleFaint.Render(Coin(frame))
	if popping(o.CoinAge) {
		slot = styleCoin.Render("+" + money(o.CoinCents) + " " + Pip(o.CoinAge))
	}
	slot = padLeft(slot, popSlotWidth)
	blankSlot := strings.Repeat(" ", popSlotWidth)

	// The wordmark sits on this creature's own centre row rather than the
	// band's, since species differ in height.
	titleRow := buddy.TitleRow

	out := make([]string, len(face))
	for i, faceRow := range face {
		lead, gap := "", blankSlot
		if i == titleRow {
			lead, gap = wordmark, slot
		}
		out[i] = pad(lead, gap+"  "+faceRow, contentWidth())
	}
	return out
}

func cycleTag(s stats.Snapshot, now time.Time) string {
	if s.CycleEnd.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d DAYS LEFT", s.DaysLeft(now))
}

func eventTag(s stats.Snapshot) string {
	return fmt.Sprintf("%d EVENTS", s.EventsConsidered)
}

// gaugeLine is the HP bar, the headline of the whole HUD.
func gaugeLine(s stats.Snapshot, hp float64, low bool) string {
	if s.Unlimited {
		return styleLabel.Render(gaugeLabel) +
			styleBarFill.Render(strings.Repeat(blockFull, hpBarWidth)) +
			"   " + styleValue.Render("∞")
	}

	barStyle := styleBarFill
	if low {
		barStyle = styleBarAlarm
	}

	// The gauge animates from the eased value, but the percentage shows the
	// true one — a number drifting toward its target looks like a bug.
	return styleLabel.Render(gaugeLabel) +
		StyledBar(hp, hpBarWidth, barStyle, styleTrack) +
		"   " + barStyle.Bold(true).Render(fmt.Sprintf("%3.0f%%", s.FractionLeft*100))
}

// figuresLine carries the dollars, which is what most glances actually want.
func figuresLine(s stats.Snapshot) string {
	left := styleFigure.Render(money(float64(s.RemainingCents))) +
		styleLabel.Render(" left") +
		styleFaint.Render("  of "+money(float64(s.LimitCents)))
	right := styleFaint.Render("spent ") + styleMoney.Render(money(float64(s.SpentCents)))
	return pad(left, right, contentWidth())
}

func modelLines(s stats.Snapshot) []string {
	if len(s.TopModels) == 0 {
		return []string{styleFaint.Render("no billed usage yet this cycle")}
	}

	// Fixed columns so bars and figures stay put between frames. A block that
	// reflows as values change is unreadable at a glance, which defeats the
	// point of the tool.
	out := make([]string, 0, len(s.TopModels))
	for i, m := range s.TopModels {
		name := ShortModel(m.Model)
		if lipgloss.Width(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}

		left := fmt.Sprintf("%s %s  %s  %s",
			styleFaint.Render(fmt.Sprintf("%d.", i+1)),
			styleValue.Render(fmt.Sprintf("%-*s", nameWidth, name)),
			StyledBar(m.Share, modelBarWidth, styleLabel, styleTrack),
			styleFaint.Render(fmt.Sprintf("%5.1f%%", m.Share*100)),
		)
		// Costs hug the right so the column reads as a column.
		out = append(out, pad(left, styleMoney.Render(money(m.Cents)), contentWidth()))
	}
	return out
}

// burnLine always shows the burn rate — it is what the tool is named for — and
// adds a projection or a warning depending on where that rate leads.
//
// The figure is labelled "avg" and is the cycle average: total spend divided by
// elapsed cycle time. It deliberately is NOT the instantaneous rate. Two
// reasons. An unlabelled "$36/day" reads as a measured daily total rather than
// a derived rate, and the instantaneous figure swings wildly — fifteen minutes
// of heavy use extrapolates to a number you will never actually spend. It also
// has to match the projection beside it, which is average-based; showing a
// burst rate next to an average-based forecast made the two disagree.
//
// The instantaneous rate still drives the machine, where a fast-moving belt
// claims nothing specific.
func burnLine(s stats.Snapshot, now time.Time) string {
	// A projected run-out date in the past reads as a bug, so an already-empty
	// budget says the real thing instead.
	if !s.Unlimited && s.RemainingCents <= 0 {
		return styleAlarm.Render("✖  BUDGET EXHAUSTED") +
			styleFaint.Render("   ·   resets "+s.CycleEnd.Local().Format("Jan 2"))
	}

	rate := s.BurnRateCentsPerDay(now)
	if rate <= 0 {
		return styleFaint.Render("◆  no spend yet this cycle")
	}

	resets := ""
	if !s.CycleEnd.IsZero() {
		resets = ", resets " + s.CycleEnd.Local().Format("Jan 2")
	}

	if when := s.RunsOutOn(now); !when.IsZero() {
		return styleWarn.Render("▲  avg "+money(rate)+"/day") +
			styleFaint.Render("   ·   empty by "+when.Local().Format("Mon Jan 2")+resets)
	}

	left := styleLabel.Render("◆  avg " + money(rate) + "/day")
	if proj := s.ProjectedCents(now); proj > 0 {
		return left + styleFaint.Render("   ·   on pace for "+money(proj)+
			" by "+s.CycleEnd.Local().Format("Jan 2"))
	}
	return left
}

// legend spells out the cycling keys, and names what each is currently set to
// so the labels double as state.
func legend(o Opts) string {
	if !o.Arcade {
		return styleFaint.Render("m") + styleBorder.Render(" machine   ") +
			styleFaint.Render("b") + styleBorder.Render(" buddy: ") +
			styleLabel.Render(BuddyAt(o.Buddy).Name) +
			styleBorder.Render("   ") +
			styleFaint.Render("s") + styleBorder.Render(" save look")
	}
	return styleFaint.Render("m") + styleBorder.Render(" machine: ") +
		styleLabel.Render(MachineAt(o.Machine).Name()) +
		styleBorder.Render("   ") +
		styleFaint.Render("b") + styleBorder.Render(" buddy: ") +
		styleLabel.Render(BuddyAt(o.Buddy).Name) +
		styleBorder.Render("   ") +
		styleFaint.Render("s") + styleBorder.Render(" save look")
}

func footer(s stats.Snapshot, o Opts) string {
	mode := "a machine on"
	if o.Arcade {
		mode = "a machine off"
	}
	left := styleFaint.Render("r refresh  ·  q quit  ·  " + mode)

	// A save confirmation takes the right-hand slot for a few seconds. It
	// matters that this is visible: writing a config file with no feedback
	// leaves you unsure whether the keypress registered.
	if o.Saved != "" {
		return pad(left, styleLabel.Render(o.Saved), contentWidth())
	}

	// No clock. The HUD polls on its own, so a timestamp is noise; the only
	// thing worth saying about freshness is when it has stopped being fresh.
	if !s.Stale {
		return left
	}
	return pad(left, styleWarn.Render("STALE"), contentWidth())
}

// center puts content in the middle of a fixed-width row.
func center(content string, width int) string {
	slack := width - lipgloss.Width(content)
	if slack <= 0 {
		return content
	}
	return strings.Repeat(" ", slack/2) + content
}

// padLeft right-aligns content in a fixed-width field.
func padLeft(content string, width int) string {
	if pad := width - lipgloss.Width(content); pad > 0 {
		return strings.Repeat(" ", pad) + content
	}
	return content
}

// pad places left and right at opposite ends of a fixed-width row.
func pad(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// ShortModel trims a model name down to something that fits the cabinet.
//
// The API mixes two formats — slugs like "claude-opus-5-thinking-high" and
// display names like "Cursor Grok 4.6 (Auto Balanced)". This shortens rather
// than maps, because Cursor renames models often and a lookup table would
// silently show stale labels.
func ShortModel(m string) string {
	if m == "" {
		return "unknown"
	}
	s := strings.TrimSuffix(m, " (Auto Balanced)")
	s = strings.TrimPrefix(s, "Cursor ")
	s = strings.TrimPrefix(s, "cursor-")
	s = strings.ReplaceAll(s, "-thinking-", " ")
	s = strings.ReplaceAll(s, "-", " ")
	return s
}
