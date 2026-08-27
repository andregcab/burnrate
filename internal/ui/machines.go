package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The machines.
//
// Each one is a different picture of the same fact: your budget being consumed.
// All of them read Snapshot.ActivityRate, which blends the instantaneous
// 15-minute burst with the cycle average — so firing up a max-mode agent makes
// the machine visibly work within a minute or two, while steady use keeps it
// running rather than letting it fall asleep.
//
// Every machine renders exactly one row, so cycling between them never reflows
// the cabinet.

// FullScaleCentsPerDay is the burn rate at which the machines run flat out.
//
// Provisional. It is a stand-in for a baseline we do not have yet, and nothing
// user-visible states it as fact — it only scales how fast the art moves.
const FullScaleCentsPerDay = 3000

const (
	holdIdle = 14 // frames per step when nothing is burning
	holdMax  = 2  // frames per step at redline

)

// Machine draws one animated row.
type Machine interface {
	// Name is the section label.
	Name() string
	// Row renders the machine at the given width.
	Row(width, frame int, activity float64, firing bool) string
}

// Machines is the cycle order, chosen so the first is the most on-theme.
var Machines = []Machine{Furnace{}, Factory{}, Reactor{}, Pumpjack{}}

// MachineSlug is a machine's config-friendly name, derived from its display
// name so adding a machine needs no second identifier to keep in sync.
func MachineSlug(m Machine) string {
	return strings.ToLower(strings.ReplaceAll(m.Name(), " ", "-"))
}

// MachineIndexBySlug finds a machine by slug, returning -1 if there is none.
//
// Defaults are stored by slug rather than by index so that reordering the
// catalog, or inserting a machine, cannot silently change someone's saved
// choice into a different machine.
func MachineIndexBySlug(slug string) int {
	for i, m := range Machines {
		if MachineSlug(m) == slug {
			return i
		}
	}
	return -1
}

// MachineSlugs lists every machine, for error messages.
func MachineSlugs() string {
	out := make([]string, len(Machines))
	for i, m := range Machines {
		out[i] = MachineSlug(m)
	}
	return strings.Join(out, ", ")
}

// hold maps activity 0..1 onto frames-per-step. Higher activity, faster motion.
func hold(activity float64) int {
	if activity <= 0 {
		return holdIdle
	}
	if activity > 1 {
		activity = 1
	}
	h := int(float64(holdIdle) - activity*float64(holdIdle-holdMax) + 0.5)
	if h < holdMax {
		return holdMax
	}
	return h
}

// Tier buckets activity into a coarse level that machines use to pick their art
// — how tall the furnace's flames stand, whether the reactor core runs hot.
//
// Deliberately unlabelled. These thresholds are a guess at what counts as a
// heavy day, and we have no basis for that guess yet: one account's normal is
// another's emergency. Driving art off them is harmless, since a bigger flame
// only ever implies "more than a moment ago". Printing a word like "MAX BURN"
// next to it would be a claim, and we cannot support one.
//
// When there is enough history to know a person's own baseline, this should
// become relative to that rather than to a fixed dollar figure.
func Tier(activity float64) int {
	switch {
	case activity <= 0:
		return 0
	case activity < 0.15:
		return 1
	case activity < 0.40:
		return 2
	case activity < 0.75:
		return 3
	default:
		return 4
	}
}

// ── FURNACE ─────────────────────────────────────────────────────────────────

// Furnace burns money. Bills feed in from the right, flames lick above the
// firebox, and the fire grows with the burn rate.
type Furnace struct{}

func (Furnace) Name() string { return "MONEY FURNACE" }

// flameSets are ordered by intensity: a bigger fire for a bigger burn.
var flameSets = [][]string{
	{"·", " ", "·", " "},
	{"▁", "·", "▁", "▂"},
	{"▂", "▃", "▂", "▁"},
	{"▃", "▅", "▄", "▆"},
	{"▅", "▇", "▆", "█"},
}

func (Furnace) Row(width, frame int, activity float64, firing bool) string {
	h := hold(activity)
	step := frame / h

	flames := flameSets[Tier(activity)]

	// The firebox brightens as it works.
	boxStyle := styleBorder
	if Tier(activity) >= 3 {
		boxStyle = styleMoney
	}
	if firing {
		boxStyle = styleAlarm
	}

	var fire strings.Builder
	for i := 0; i < 5; i++ {
		fire.WriteString(flames[mod(i+step, len(flames))])
	}

	box := boxStyle.Render("▛" + fire.String() + "▜")
	left := box + " " + styleFaint.Render("╨")

	// Bills queue up on the right and march toward the fire. They are the
	// budget, visibly going in.
	feedW := width - lipgloss.Width(left) - 1
	if feedW < 1 {
		feedW = 1
	}
	var feed strings.Builder
	for i := 0; i < feedW; i++ {
		if mod(i+step, 6) == 0 {
			feed.WriteString("$")
		} else {
			feed.WriteString("·")
		}
	}
	return left + " " + styleFaint.Render(feed.String())
}

// ── FACTORY ─────────────────────────────────────────────────────────────────

// Factory is the piston-and-conveyor machine: two housings, a piston head that
// fires when spend lands, and a belt that scrolls with the burn rate.
type Factory struct{}

func (Factory) Name() string { return "TOKEN FACTORY" }

// tokenSpin is one arcade token rotating edge-on, the way a coin does in a
// side-scroller. Four frames read as a full turn.
var tokenSpin = []string{"◉", "◐", "▮", "◑"}

// tokenSpacing is cells between tokens on the belt. Five, not four: a spacing
// that divides evenly into the spin cycle makes every token turn in lockstep,
// which reads as a repeating texture rather than as separate objects.
const tokenSpacing = 5

// The factory head is a pump between two geared housings. Everything in it runs
// continuously off the belt's step counter, so the machine works whenever the
// belt moves — previously only the piston moved, and only on spend, which left
// the head sitting dead while tokens streamed past it.
var (
	// pistonStroke is one full pump cycle. Six frames so it does not alias
	// against the four-frame token spin.
	pistonStroke = []string{"▂▄▂", "▃▅▃", "▄▆▄", "▅▇▅", "▄▆▄", "▃▅▃"}

	// pistonFired is the head at full extension when spend lands.
	pistonFired = "▀█▀"

	// gearTeeth rotate in the housings on either side.
	gearTeeth = []string{"◐", "◓", "◑", "◒"}
)

func (Factory) Row(width, frame int, activity float64, firing bool) string {
	step := frame / hold(activity)

	head, headStyle := pistonStroke[mod(step, len(pistonStroke))], styleLabel
	if firing {
		head, headStyle = pistonFired, styleMoney
	}

	// The two gears counter-rotate, which reads as meshing rather than as two
	// unrelated spinners.
	left := gearTeeth[mod(step, len(gearTeeth))]
	right := gearTeeth[mod(-step, len(gearTeeth))]

	gearStyle := styleBorder
	if Tier(activity) >= 3 {
		gearStyle = styleLabel
	}

	machine := styleBorder.Render("▛") + gearStyle.Render(left) + styleBorder.Render("▜") +
		" " + headStyle.Render(head) + " " +
		styleBorder.Render("▛") + gearStyle.Render(right) + styleBorder.Render("▜")
	beltW := width - lipgloss.Width(machine) - 1
	if beltW < 1 {
		beltW = 1
	}

	// Build the belt cell by cell so tokens can be styled individually — they
	// are the things being consumed, and should read brighter than the rail
	// carrying them.
	// i+step, not i-step: the machine sits at the left, so tokens have to travel
	// leftward into it. Subtracting marched them away from the furnace, which
	// read as the machine producing money rather than consuming it.
	var b strings.Builder
	for i := 0; i < beltW; i++ {
		if mod(i+step, tokenSpacing) != 0 {
			b.WriteString(styleBorder.Render("─"))
			continue
		}
		// Each token's phase is offset by its position, so they spin
		// independently instead of flashing in unison.
		spin := tokenSpin[mod(step+i/tokenSpacing, len(tokenSpin))]

		// The token nearest the machine is the one being consumed right now.
		if firing && i < tokenSpacing {
			b.WriteString(styleAlarm.Render(spin))
			continue
		}
		b.WriteString(styleMoney.Render(spin))
	}
	return machine + " " + b.String()
}

// ── REACTOR ─────────────────────────────────────────────────────────────────

// Reactor is a containment vessel: a core inside pulsing shield rings, flanked
// by control rods and coolant pumps, venting spent energy down a conduit.
type Reactor struct{}

func (Reactor) Name() string { return "REACTOR CORE" }

// shieldRings expand and contract around the core. Every frame is five cells
// wide so the vessel never changes size as it pulses.
var shieldRings = []string{
	"  ◉  ",
	" (◉) ",
	"((◉))",
	" (◉) ",
}

// controlRods rise and fall in their channels.
var controlRods = []string{"╿", "│", "╽", "│"}

// coolantPumps counter-rotate on either side of the vessel.
var coolantPumps = []string{"◍", "◌", "◍", "○"}

func (Reactor) Row(width, frame int, activity float64, firing bool) string {
	step := frame / hold(activity)

	coreStyle := styleLabel
	if Tier(activity) >= 3 {
		coreStyle = styleMoney
	}
	if firing {
		coreStyle = styleAlarm
	}

	rings := shieldRings[mod(step, len(shieldRings))]
	rodL := controlRods[mod(step, len(controlRods))]
	rodR := controlRods[mod(-step, len(controlRods))]
	pumpL := coolantPumps[mod(step, len(coolantPumps))]
	pumpR := coolantPumps[mod(-step, len(coolantPumps))]

	rodStyle := styleBorder
	if Tier(activity) >= 2 {
		rodStyle = styleLabel
	}

	vessel := styleBorder.Render(pumpL+"▐") + rodStyle.Render(rodL) +
		coreStyle.Render(rings) +
		rodStyle.Render(rodR) + styleBorder.Render("▌"+pumpR)

	condW := width - lipgloss.Width(vessel) - 1
	if condW < 1 {
		condW = 1
	}

	// Spent energy vents away from the vessel. The 7-cell period is odd on
	// purpose so it does not alias against the 4-frame core pulse.
	var b strings.Builder
	for i := 0; i < condW; i++ {
		switch mod(i+step, 7) {
		case 0:
			b.WriteString("◄")
		case 1, 2:
			b.WriteString("─")
		default:
			b.WriteString("┄")
		}
	}
	return vessel + " " + styleFaint.Render(b.String())
}

// ── PUMPJACK ────────────────────────────────────────────────────────────────

// Pumpjack is a nodding derrick drawing the budget out of the ground: a
// counterweight crank driving a walking beam over a wellhead.
type Pumpjack struct{}

func (Pumpjack) Name() string { return "PUMPJACK" }

// rigStroke is one full pump cycle. Each frame is the same width so the derrick
// does not jitter sideways as it nods. Six frames give the beam a visible pause
// at the top and bottom of its travel, the way a real one hangs at each end.
var rigStroke = []string{
	"◐╤╱▔▔╲╤╨",
	"◓╤─▔▔─╤╨",
	"◑╤╲▁▁╱╤╨",
	"◒╤─▁▁─╤╨",
	"◐╤╱▔▔╲╤╨",
	"◓╤─▔▔─╤╨",
}

func (Pumpjack) Row(width, frame int, activity float64, firing bool) string {
	step := frame / hold(activity)

	rig := rigStroke[mod(step, len(rigStroke))]

	rigStyle := styleLabel
	if Tier(activity) >= 3 {
		rigStyle = styleMoney
	}
	if firing {
		rigStyle = styleAlarm
	}

	rendered := styleBorder.Render("▐") + rigStyle.Render(rig) + styleBorder.Render("▌")

	flowW := width - lipgloss.Width(rendered) - 1
	if flowW < 1 {
		flowW = 1
	}

	// Drawn-off crude trails away from the wellhead.
	var b strings.Builder
	for i := 0; i < flowW; i++ {
		switch mod(i+step, 6) {
		case 0:
			b.WriteString("◍")
		case 1:
			b.WriteString("~")
		default:
			b.WriteString("~")
		}
	}
	return rendered + " " + styleFaint.Render(b.String())
}
