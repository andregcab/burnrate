package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/andregcab/burnrate/internal/stats"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func snap(remaining, limit int) stats.Snapshot {
	return stats.Snapshot{
		SpentCents:     limit - remaining,
		LimitCents:     limit,
		RemainingCents: remaining,
		FractionLeft:   float64(remaining) / float64(limit),
		CycleStart:     time.Now().Add(-48 * time.Hour),
		CycleEnd:       time.Now().Add(29 * 24 * time.Hour),
		FetchedAt:      time.Now(),
	}
}

func TestBarRoundsToWholeCells(t *testing.T) {
	F, E := blockFull, blockEmpty

	tests := []struct {
		name  string
		frac  float64
		width int
		want  string
	}{
		{"empty", 0, 4, E + E + E + E},
		{"full", 1, 4, F + F + F + F},
		{"half of four", 0.5, 4, F + F + E + E},
		{"rounds down", 0.30, 4, F + E + E + E},
		{"rounds up", 0.40, 4, F + F + E + E},
		{"negative clamps", -5, 4, E + E + E + E},
		{"over one clamps", 5, 4, F + F + F + F},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Bar(tt.frac, tt.width); got != tt.want {
				t.Errorf("Bar(%v, %d) = %q, want %q", tt.frac, tt.width, got, tt.want)
			}
		})
	}
}

// Real spend must never render as an empty bar. Rounding a small-but-nonzero
// share down to nothing would read as "this model went unused", which is worse
// than overstating it by a fraction of a cell.
func TestTinyButNonzeroShareStillShowsOneCell(t *testing.T) {
	for _, frac := range []float64{0.0001, 0.01, 0.02} {
		got := Bar(frac, 16)
		if !strings.HasPrefix(got, blockFull) {
			t.Errorf("Bar(%v, 16) = %q, want at least one filled cell", frac, got)
		}
	}
	// Genuinely zero must still be empty.
	if got := Bar(0, 16); strings.Contains(got, blockFull) {
		t.Errorf("Bar(0, 16) = %q, want no filled cells", got)
	}
}

// Every gauge must occupy exactly its declared width. Half blocks and full
// blocks differ in byte length, so a naive width calculation leaves the
// cabinet's right wall ragged at one specific value.
func TestBarWidthIsConstant(t *testing.T) {
	for i := 0; i <= 100; i++ {
		got := Bar(float64(i)/100, hpBarWidth)
		if w := lipgloss.Width(got); w != hpBarWidth {
			t.Fatalf("Bar(%.2f) rendered %d cells, want %d", float64(i)/100, w, hpBarWidth)
		}
	}
}

func TestShortModel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"gpt-5.6-sol-xhigh", "gpt 5.6 sol xhigh"},
		{"claude-opus-5-thinking-high", "claude opus 5 high"},
		{"Cursor Grok 4.6 (Auto Balanced)", "Grok 4.6"},
		{"Claude Opus 5 (Auto Balanced)", "Claude Opus 5"},
		{"cursor-grok-4.6-high", "grok 4.6 high"},
		{"composer-2.5", "composer 2.5"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		if got := ShortModel(tt.in); got != tt.want {
			t.Errorf("ShortModel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A projected run-out date in the past reads as a bug, so an already-empty
// budget says the real thing instead.
func TestExhaustedBudgetSaysSoRatherThanProjectingAPastDate(t *testing.T) {
	out := burnLine(snap(0, 30000), time.Now())
	if !strings.Contains(out, "EXHAUSTED") {
		t.Errorf("trajectory at zero remaining = %q, want an EXHAUSTED notice", out)
	}
	if strings.Contains(out, "empty by") {
		t.Error("trajectory projected a run-out date for an already-empty budget")
	}
}

// A healthy budget still shows the rate — it is the app's namesake — but must
// not threaten the user with a run-out date.
func TestHealthyBudgetShowsRateWithoutWarning(t *testing.T) {
	out := stripANSI(burnLine(snap(29900, 30000), time.Now()))
	if strings.Contains(out, "empty by") {
		t.Errorf("burnLine = %q, want no run-out warning for a healthy budget", out)
	}
	if !strings.Contains(out, "/day") {
		t.Errorf("burnLine = %q, want the burn rate shown", out)
	}
}

// The empty track must stay shaded. Drawing it from a dimmed █ made adjacent
// model bars fuse into one solid rectangle, which was worse than the font
// mismatch it fixed.
func TestStyledBarKeepsAShadedTrack(t *testing.T) {
	for _, frac := range []float64{0, 0.25, 0.5, 0.75} {
		out := stripANSI(StyledBar(frac, 16, styleValue, styleTrack))
		if !strings.Contains(out, blockEmpty) {
			t.Errorf("StyledBar(%.2f) = %q, want a shaded track", frac, out)
		}
		for _, r := range out {
			switch string(r) {
			case blockFull, blockEmpty:
			default:
				t.Errorf("StyledBar(%.2f) contains unexpected glyph %q", frac, string(r))
			}
		}
		if w := lipgloss.Width(out); w != 16 {
			t.Errorf("StyledBar(%.2f) is %d cells, want 16", frac, w)
		}
	}
	// A full bar has no track left to shade.
	if strings.Contains(stripANSI(StyledBar(1, 16, styleValue, styleTrack)), blockEmpty) {
		t.Error("a full bar should have no shaded cells")
	}
}

// The factory head must move on its own. Previously only the piston animated,
// and only when spend landed, so the machine sat dead while tokens streamed past.
func TestFactoryHeadAnimatesWithoutSpend(t *testing.T) {
	// Compare only the machine head, not the belt — the belt was already
	// animating, and would mask a dead head.
	head := func(f int) string {
		r := []rune(stripANSI(Factory{}.Row(40, f, 0.5, false)))
		if len(r) > 11 {
			r = r[:11]
		}
		return string(r)
	}

	first := head(0)
	moved := false
	for f := 1; f < 60; f++ {
		if head(f) != first {
			moved = true
			break
		}
	}
	if !moved {
		t.Error("the factory head never moved across 60 frames with no spend")
	}
}

// All animation flows from the frame counter, never from wall-clock time. That
// is what makes every other test in this file possible.
func TestRenderIsDeterministicForAFrame(t *testing.T) {
	s := snap(15000, 30000)
	now := time.Now()
	o := Opts{Arcade: true, HP: 0.5, CoinAge: -1}

	first := Render(s, now, 7, o)
	for i := 0; i < 5; i++ {
		if got := Render(s, now, 7, o); got != first {
			t.Fatal("Render produced different output for the same frame")
		}
	}
}

// Every rendered line must be exactly Width cells, in both modes and at every
// frame, or the cabinet goes ragged as the rain falls.
func TestEveryLineIsExactlyWidth(t *testing.T) {
	for _, arcade := range []bool{false, true} {
		for _, hp := range []float64{1, 0.5, 0.14, 0} {
			for frame := 0; frame < 40; frame += 7 {
				out := Render(snap(int(hp*30000), 30000), time.Now(), frame,
					Opts{Arcade: arcade, HP: hp, CoinAge: -1})
				for i, ln := range strings.Split(out, "\n") {
					if w := lipgloss.Width(ln); w != Width {
						t.Fatalf("arcade=%v hp=%.2f frame=%d line %d is %d cells, want %d\n%s",
							arcade, hp, frame, i, w, Width, ln)
					}
				}
			}
		}
	}
}

// A coin pop must not change the row width, or the title bar reflows whenever
// spend ticks up.
func TestCoinPopDoesNotReflowTitleBar(t *testing.T) {
	s := snap(15000, 30000)
	now := time.Now()
	base := Render(s, now, 3, Opts{Arcade: true, HP: 0.5, CoinAge: -1})
	pop := Render(s, now, 3, Opts{Arcade: true, HP: 0.5, CoinAge: 0, CoinCents: 1004})

	bl := lipgloss.Width(strings.Split(base, "\n")[1])
	pl := lipgloss.Width(strings.Split(pop, "\n")[1])
	if bl != pl {
		t.Errorf("title bar is %d cells without a pop and %d with one", bl, pl)
	}
}

// Spend pops are money and must read as money. "+1004¢" is not money.
func TestCoinPopFormatsAsDollars(t *testing.T) {
	out := Render(snap(15000, 30000), time.Now(), 3,
		Opts{Arcade: true, HP: 0.5, CoinAge: 0, CoinCents: 1004})
	if !strings.Contains(out, "$10.04") {
		t.Errorf("coin pop did not render as $10.04:\n%s", out)
	}
	if strings.Contains(out, "1004") {
		t.Error("coin pop rendered raw cents instead of dollars")
	}
}

// The blink itself should look normal; it is the spacing that should be long.
func TestBlinkIsFastButRare(t *testing.T) {
	if secs := float64(blinkFrames) / fps; secs > 0.4 {
		t.Errorf("a blink lasts %.2fs, want a normal-speed blink under 0.4s", secs)
	}

	var onsets []int
	prev := false
	for f := 0; f < blinkCycle; f++ {
		shut := blinking(f)
		if shut && !prev {
			onsets = append(onsets, f)
		}
		prev = shut
	}
	if len(onsets) < 5 {
		t.Fatalf("found %d blinks in a cycle, want many", len(onsets))
	}

	seen := map[int]bool{}
	for i := 1; i < len(onsets); i++ {
		gap := (onsets[i] - onsets[i-1]) / fps
		if gap < blinkGapMinSec || gap > blinkGapMaxSec {
			t.Errorf("blink gap of %ds is outside the %d-%ds range",
				gap, blinkGapMinSec, blinkGapMaxSec)
		}
		seen[gap] = true
	}
	// A single repeated gap would read as a metronome rather than as a face.
	if len(seen) < 2 {
		t.Error("every blink gap is identical; spacing should vary")
	}
}

// The machine must actually move, or it looks broken rather than idle.
func TestMachineAnimates(t *testing.T) {
	for _, m := range Machines {
		first := stripANSI(m.Row(50, 0, 0.5, false))
		moved := false
		for f := 1; f < 60; f++ {
			if stripANSI(m.Row(50, f, 0.5, false)) != first {
				moved = true
				break
			}
		}
		if !moved {
			t.Errorf("%s never changed across 60 frames", m.Name())
		}
	}
}

// Speed tracking is the whole point: the machine must react to burn rate, not
// just spin at a constant rate.
func TestMachineSpeedTracksActivity(t *testing.T) {
	idle, low, hot := hold(0), hold(0.15), hold(1)
	if !(idle > low && low > hot) {
		t.Errorf("hold does not shorten with activity: idle=%d low=%d hot=%d", idle, low, hot)
	}
	// An out-of-range activity must not drive the hold to zero and divide by it.
	if h := hold(500); h < holdMax {
		t.Errorf("absurd activity gave hold %d, want at least %d", h, holdMax)
	}
	if h := hold(-5); h != holdIdle {
		t.Errorf("negative activity gave hold %d, want %d", h, holdIdle)
	}
}

// Every machine renders exactly one row of exactly the requested width, so
// cycling between them never reflows the cabinet.
func TestMachineRowsAreUniform(t *testing.T) {
	for _, m := range Machines {
		for _, act := range []float64{0, 0.5, 1} {
			for _, firing := range []bool{false, true} {
				for f := 0; f < 30; f += 7 {
					got := m.Row(58, f, act, firing)
					if strings.Contains(got, "\n") {
						t.Fatalf("%s rendered more than one row", m.Name())
					}
					if w := lipgloss.Width(got); w != 58 {
						t.Errorf("%s at act=%.1f firing=%v frame=%d is %d cells, want 58",
							m.Name(), act, firing, f, w)
					}
				}
			}
		}
	}
}

// Every buddy, in every body frame and eye state, must occupy exactly the same
// box. Cycling with `b`, catching a blink, or putting on a hat must never
// resize the title band and reflow the cabinet underneath it.
func TestAllBuddyPosesShareOneFootprint(t *testing.T) {
	for _, b := range Buddies {
		for _, low := range []bool{false, true} {
			for f := 0; f < bodyHold*4; f += 3 {
				face := b.Face(f, low)
				if len(face) != BuddyRows {
					t.Fatalf("buddy %q has %d rows, want %d", b.Name, len(face), BuddyRows)
				}
				for ri, row := range face {
					if w := lipgloss.Width(stripANSI(row)); w != BuddyWidth {
						t.Errorf("buddy %q row %d is %d cells, want %d (%q)",
							b.Name, ri, w, BuddyWidth, stripANSI(row))
					}
				}
			}
		}
	}
}

// The body animation has to actually cycle, or the pet is a still image.
func TestBuddyBodyAnimates(t *testing.T) {
	for _, b := range Buddies {
		if len(b.Frames) < 2 {
			continue
		}
		first := strings.Join(b.Face(0, false), "|")
		moved := false
		for f := 1; f < bodyHold*len(b.Frames)+1; f++ {
			if strings.Join(b.Face(f, false), "|") != first {
				moved = true
				break
			}
		}
		if !moved {
			t.Errorf("buddy %q never changed across a full body cycle", b.Name)
		}
	}
}

// Every sprite must carry at least one eye, or the blink and alarm states have
// nothing to act on.
func TestEverySpriteHasEyes(t *testing.T) {
	for _, b := range Buddies {
		for fi, frame := range b.Frames {
			if !strings.Contains(strings.Join(frame, ""), eyePlaceholder) {
				t.Errorf("buddy %q frame %d contains no eye placeholder", b.Name, fi)
			}
		}
	}
}

// The title band must be exactly as tall as a buddy, however wide the spend pop
// happens to be.
func TestTitleBandHeightIsStable(t *testing.T) {
	s := snap(15000, 30000)
	for buddy := 0; buddy < len(Buddies); buddy++ {
		for _, coin := range []int{-1, 0, 3} {
			band := titleBand(s, 0, false, Opts{
				Buddy: buddy, CoinAge: coin, CoinCents: 10000,
			})
			if len(band) != BuddyRows {
				t.Fatalf("buddy %d coin %d: band is %d rows, want %d",
					buddy, coin, len(band), BuddyRows)
			}
			for i, ln := range band {
				if w := lipgloss.Width(ln); w != contentWidth() {
					t.Errorf("buddy %d coin %d row %d is %d cells, want %d",
						buddy, coin, i, w, contentWidth())
				}
			}
		}
	}
}

// A spend pop must not shove the sprite sideways — the pop text is far wider
// than the spinner it replaces.
func TestSpendPopDoesNotDisplaceTheSprite(t *testing.T) {
	s := snap(15000, 30000)
	quiet := titleBand(s, 0, false, Opts{Buddy: 0, CoinAge: -1})
	popped := titleBand(s, 0, false, Opts{Buddy: 0, CoinAge: 0, CoinCents: 10000})

	for i := range quiet {
		a := len(stripANSI(quiet[i])) - len(strings.TrimRight(stripANSI(quiet[i]), " "))
		b := len(stripANSI(popped[i])) - len(strings.TrimRight(stripANSI(popped[i]), " "))
		if a != b {
			t.Errorf("row %d: sprite sits at a different offset with a pop (%d vs %d)", i, a, b)
		}
	}
}

// Cycling wraps rather than panicking at the end of the catalog.
func TestCatalogIndexesWrap(t *testing.T) {
	for _, i := range []int{-7, -1, 0, 3, 99} {
		if BuddyAt(i).Name == "" {
			t.Errorf("BuddyAt(%d) returned an empty buddy", i)
		}
		if MachineAt(i).Name() == "" {
			t.Errorf("MachineAt(%d) returned an unnamed machine", i)
		}
	}
}

func TestCalmModeHasNoMachine(t *testing.T) {
	out := stripANSI(Render(snap(15000, 30000), time.Now(), 11,
		Opts{Arcade: false, HP: 0.5, CoinAge: -1}))
	for _, m := range Machines {
		if strings.Contains(out, m.Name()) {
			t.Errorf("calm mode rendered the %s section", m.Name())
		}
	}
}

func TestBuddySwitchesToAlarmAtLowHP(t *testing.T) {
	for _, b := range Buddies {
		calm := stripANSI(strings.Join(b.Face(0, false), "\n"))
		panicked := stripANSI(strings.Join(b.Face(0, true), "\n"))
		if calm == panicked {
			t.Errorf("buddy %q shows the same face healthy and alarmed", b.Name)
		}
	}
}

// Blinking has to actually swap the eyes, or the schedule is wired to nothing.
func TestBlinkClosesTheEyes(t *testing.T) {
	// Pick an open frame and a shut frame that land in the same body pose, so
	// the body animation is not what differs between them.
	var open, shut = -1, -1
	for f := 0; f < blinkCycle && (open < 0 || shut < 0); f++ {
		if blinking(f) {
			if shut < 0 {
				shut = f
			}
			continue
		}
		if open < 0 || open/bodyHold != shut/bodyHold {
			open = f
		}
	}
	if open < 0 || shut < 0 {
		t.Fatal("blink schedule never opens or never shuts")
	}

	for _, b := range Buddies {
		o := stripANSI(strings.Join(b.Face(shut-1, false), "|"))
		c := stripANSI(strings.Join(b.Face(shut, false), "|"))

		if !strings.Contains(o, eyeOpen) {
			t.Errorf("buddy %q has no open eye just before a blink", b.Name)
		}
		if strings.Contains(c, eyeOpen) {
			t.Errorf("buddy %q still shows an open eye mid-blink", b.Name)
		}
		if !strings.Contains(c, eyeShut) {
			t.Errorf("buddy %q shows no shut eye mid-blink", b.Name)
		}
	}
}

// Blinking has to actually change the art, or the schedule is wired to nothing.
func TestBlinkChangesTheFace(t *testing.T) {
	// Find a frame inside a blink and one outside it.
	var open, shut int = -1, -1
	for f := 0; f < blinkCycle; f++ {
		if blinking(f) && shut < 0 {
			shut = f
		}
		if !blinking(f) && open < 0 {
			open = f
		}
	}
	if open < 0 || shut < 0 {
		t.Fatal("blink schedule never opens or never shuts")
	}

	for _, b := range Buddies {
		// Compare within one body frame so the body animation is not the thing
		// that differs.
		o := stripANSI(strings.Join(b.Face(open, false), "|"))
		sft := stripANSI(strings.Join(b.Face(open+(shut-open)%bodyHold, false), "|"))
		_ = sft
		withEye := strings.Contains(o, eyeOpen)
		if !withEye {
			t.Errorf("buddy %q shows no open eye outside a blink", b.Name)
		}
	}
}

// Art tiers must stay inside the range every machine draws from, so a wilder
// burn than expected cannot index past the end of the flame set.
func TestTierStaysWithinArtRange(t *testing.T) {
	for _, act := range []float64{-5, 0, 0.05, 0.2, 0.5, 0.9, 1.0, 42} {
		tier := Tier(act)
		if tier < 0 || tier >= len(flameSets) {
			t.Errorf("Tier(%.2f) = %d, outside the furnace's art range 0..%d",
				act, tier, len(flameSets)-1)
		}
	}
}

// The machine section must not label the burn rate. The thresholds behind it
// are a guess, and printing a word like "MAX BURN" states it as fact.
func TestMachineSectionShowsNoIntensityLabel(t *testing.T) {
	for _, hp := range []float64{1, 0.5, 0.1} {
		out := stripANSI(Render(snap(int(hp*30000), 30000), time.Now(), 0,
			Opts{Arcade: true, HP: hp, CoinAge: -1}))
		for _, word := range []string{"MAX BURN", "HEAVY", "STEADY", "LIGHT", "IDLE"} {
			if strings.Contains(out, word) {
				t.Errorf("HUD contains the intensity label %q", word)
			}
		}
	}
}

// No gauge may use a full block.
//
// This regressed three times. █ fills its entire line box including leading, so
// stacked filled runs fuse into one continuous shape while the shaded tails
// beside them keep a clean gap — the same bar appears to have different row
// spacing depending on how full it is. Both halves must be dither patterns,
// which stop short of the cell edges and so carry the same built-in gap.
func TestGaugesNeverUseAFullBlock(t *testing.T) {
	const fullBlock = "█"

	for _, frac := range []float64{0, 0.25, 0.5, 0.75, 1} {
		if got := Bar(frac, 16); strings.Contains(got, fullBlock) {
			t.Errorf("Bar(%.2f) = %q contains a full block", frac, got)
		}
	}

	for _, frac := range []float64{0, 0.25, 0.5, 0.75, 1} {
		got := stripANSI(StyledBar(frac, 16, styleValue, styleTrack))
		if strings.Contains(got, fullBlock) {
			t.Errorf("StyledBar(%.2f) = %q contains a full block", frac, got)
		}
	}

	// And not in the model rows, which are the ones that stack and fuse.
	// (The machines legitimately use █ for flames and piston heads, so the
	// check is scoped to gauges rather than the whole HUD.)
	s := snap(15000, 30000)
	s.TopModels = []stats.ModelUse{
		{Model: "a", Cents: 100, Events: 1, Share: 1.0},
		{Model: "b", Cents: 50, Events: 1, Share: 0.5},
		{Model: "c", Cents: 1, Events: 1, Share: 0.01},
	}
	for _, ln := range modelLines(s) {
		if strings.Contains(stripANSI(ln), fullBlock) {
			t.Errorf("model row %q contains a full block", stripANSI(ln))
		}
	}
}

// The three gauge glyphs must be visually distinct, or a bar reads as one
// undifferentiated smear.
func TestGaugeGlyphsAreDistinct(t *testing.T) {
	if blockFull == blockEmpty {
		t.Error("filled and empty gauge glyphs are identical")
	}
}

// Gauge glyphs must never be bold.
//
// Terminals render bold by thickening the glyph, and on a dither pattern that
// makes the filled run visibly heavier and taller than the track beside it, so
// the two halves of a single bar sit at different heights.
func TestGaugeFillsAreNotBold(t *testing.T) {
	for name, st := range map[string]lipgloss.Style{
		"styleBarFill":  styleBarFill,
		"styleBarAlarm": styleBarAlarm,
		"styleTrack":    styleTrack,
		"styleLabel":    styleLabel,
	} {
		if st.GetBold() {
			t.Errorf("%s is bold; gauge glyphs must not be", name)
		}
	}
}

// A saved default is stored by name, so it must survive the catalog changing
// order or gaining new companions.
func TestBuddyIndexByName(t *testing.T) {
	for i, b := range Buddies {
		if got := BuddyIndexByName(b.Name); got != i {
			t.Errorf("BuddyIndexByName(%q) = %d, want %d", b.Name, got, i)
		}
	}
	if got := BuddyIndexByName("no-such-creature"); got != -1 {
		t.Errorf("BuddyIndexByName(unknown) = %d, want -1", got)
	}
	if got := BuddyIndexByName(""); got != -1 {
		t.Errorf("BuddyIndexByName(empty) = %d, want -1", got)
	}
}

// Defaults are stored by slug, so reordering the catalog or inserting a machine
// cannot silently turn someone's saved choice into a different machine.
func TestMachineIndexBySlug(t *testing.T) {
	for i, m := range Machines {
		slug := MachineSlug(m)
		if strings.ContainsAny(slug, " ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			t.Errorf("slug %q should be lowercase with no spaces", slug)
		}
		if got := MachineIndexBySlug(slug); got != i {
			t.Errorf("MachineIndexBySlug(%q) = %d, want %d", slug, got, i)
		}
	}
	if got := MachineIndexBySlug("no-such-machine"); got != -1 {
		t.Errorf("MachineIndexBySlug(unknown) = %d, want -1", got)
	}
	if got := MachineIndexBySlug(""); got != -1 {
		t.Errorf("MachineIndexBySlug(empty) = %d, want -1", got)
	}
}

// The wordmark must land on a row where the creature actually has art, or it
// will appear to float beside an empty space.
func TestTitleRowLandsOnInk(t *testing.T) {
	for _, b := range Buddies {
		if b.TitleRow < 0 || b.TitleRow >= SpriteRows {
			t.Errorf("buddy %q: TitleRow %d is outside the box", b.Name, b.TitleRow)
			continue
		}
		inked := false
		for _, f := range b.Frames {
			if strings.TrimSpace(f[b.TitleRow]) != "" {
				inked = true
				break
			}
		}
		if !inked {
			t.Errorf("buddy %q: TitleRow %d is blank in every frame", b.Name, b.TitleRow)
		}
	}
}

// Cycling buddies must not change the band's height, or the whole cabinet
// reflows on every keypress.
func TestBandHeightIsConstantAcrossBuddies(t *testing.T) {
	snapshot := snap(15000, 30000)
	for i := range Buddies {
		band := titleBand(snapshot, 0, false, Opts{Buddy: i, CoinAge: -1})
		if len(band) != BuddyRows {
			t.Fatalf("buddy %q: band is %d rows, want %d",
				Buddies[i].Name, len(band), BuddyRows)
		}
	}
}

// The box is four rows precisely so that every creature fills it exactly, with
// no spare row to place. If a species ever occupies fewer rows it will sit
// visibly off-centre, and if one occupies more it should have been excluded.
func TestEveryBuddyFillsTheBoxExactly(t *testing.T) {
	for _, b := range Buddies {
		first, last := BuddyRows, -1
		for _, f := range b.Frames {
			for r, row := range f {
				if strings.TrimSpace(row) == "" {
					continue
				}
				if r < first {
					first = r
				}
				if r > last {
					last = r
				}
			}
		}
		if got := last - first + 1; got != BuddyRows {
			t.Errorf("buddy %q occupies %d of %d rows (rows %d..%d)",
				b.Name, got, BuddyRows, first, last)
		}
	}
}

// Species taller than the box are excluded rather than clipped, so none of the
// known five-row creatures should have survived into the roster.
func TestTallSpeciesAreExcludedNotClipped(t *testing.T) {
	tall := []string{"dragon", "octopus", "penguin", "ghost",
		"capybara", "cactus", "robot", "mushroom"}
	for _, name := range tall {
		if i := BuddyIndexByName(name); i >= 0 {
			t.Errorf("%q needs five rows but is in the roster at %d", name, i)
		}
	}
	if len(Buddies) == 0 {
		t.Fatal("every species was excluded; the box is too small")
	}
}

// The rate must be labelled as an average. Bare "$36/day" reads as a measured
// daily total rather than a derived figure.
func TestBurnLineLabelsTheRateAsAnAverage(t *testing.T) {
	out := stripANSI(burnLine(snap(15000, 30000), time.Now()))
	if !strings.Contains(out, "avg ") {
		t.Errorf("burnLine = %q, want the rate labelled as an average", out)
	}
}

// Every state must say when the budget resets, or a run-out date is impossible
// to judge — "empty by Sep 4" is alarming until you know it resets on the 1st.
func TestBurnLineAlwaysNamesTheResetDate(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
	}{
		{"healthy", 29000},
		{"burning down", 1200},
		{"exhausted", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := snap(tt.remaining, 30000)
			out := stripANSI(burnLine(s, time.Now()))
			want := s.CycleEnd.Local().Format("Jan 2")
			if !strings.Contains(out, want) {
				t.Errorf("burnLine = %q, want it to name the reset date %q", out, want)
			}
		})
	}
}
