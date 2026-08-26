package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The buddies.
//
// Sprite art comes from the Claude Code companion set (see sprites_gen.go for
// the source). Each species has three body frames; this package adds the state
// logic on top: which frame is showing, whether the eyes are open, shut or
// alarmed, and an optional hat.
//
// Every buddy occupies the same fixed box. Cycling with `b` must never change
// the HUD's dimensions, or the whole cabinet reflows underneath.

// BuddyRows and BuddyWidth are the footprint shared by every buddy.
//
// The box is four rows, one shorter than the source art's five. That makes it
// exactly as tall as a four-row creature, so those sit perfectly centred with
// no spare row to place. The cost is that species needing all five rows are
// excluded from the roster rather than clipped — a mushroom missing its cap is
// worse than a mushroom absent.
const (
	BuddyRows  = 4
	BuddyWidth = SpriteCols
)

// Eye states. The sprite data writes every eye as eyePlaceholder, so one piece
// of art covers all three without duplicating it.
const (
	eyeOpen  = "·"
	eyeShut  = "-"
	eyeAlarm = "×"
)

// bodyHold is frames per body-animation step. At 8fps that is roughly 1.5s per
// pose — a pet that breathes rather than one that twitches.
const bodyHold = 12

// Buddy is one species with its animation frames.
type Buddy struct {
	Name string

	// Frames are the body poses, each SpriteRows lines of SpriteCols cells.
	Frames [][]string

	// TitleRow is the row this creature's art is centred on.
	//
	// It varies by species because they are not all the same height: ten use
	// four rows of ink and eight use five. Aligning the wordmark to each
	// creature's own centre gives exact alignment for every one of them, which
	// a single fixed row cannot — and unlike shrinking the box, it costs no
	// art. The band's height never changes, so nothing reflows when cycling.
	TitleRow int
}

// Buddies is the cycle order, built from the generated sprite table.
var Buddies []Buddy

func init() {
	Buddies = make([]Buddy, 0, len(speciesNames))
	for _, name := range speciesNames {
		frames := speciesFrames[name]
		if len(frames) == 0 {
			continue
		}
		// Normalise defensively: a sprite whose rows are short would ragged the
		// right wall of the cabinet, and the art is generated, not hand-checked.
		norm := make([][]string, len(frames))
		for i, f := range frames {
			rows := make([]string, SpriteRows)
			for r := 0; r < SpriteRows; r++ {
				if r < len(f) {
					rows[r] = padRight(f[r], SpriteCols)
					continue
				}
				rows[r] = strings.Repeat(" ", SpriteCols)
			}
			norm[i] = rows
		}

		art, ok := fitToBox(centerFrames(norm))
		if !ok {
			// Taller than the box. Skipped rather than clipped.
			continue
		}
		Buddies = append(Buddies, Buddy{
			Name:     name,
			Frames:   art,
			TitleRow: inkCenterRow(art),
		})
	}
}

// centerFrames nudges a species' art so it sits centred in its box.
//
// The shift is computed across every frame at once and applied uniformly. Doing
// it per row or per frame would break the art apart — a sprite's rows are drawn
// relative to each other, and independently centring them would make the head
// drift off the body as it animates.
//
// Most of the source art is already centred; this exists so one badly-aligned
// species (or a future addition) does not sit visibly off to one side.
func centerFrames(frames [][]string) [][]string {
	minL, maxR := SpriteCols, 0
	for _, f := range frames {
		for _, row := range f {
			trimmed := strings.TrimRight(row, " ")
			if trimmed == "" {
				continue
			}
			if l := runeLen(row) - runeLen(strings.TrimLeft(row, " ")); l < minL {
				minL = l
			}
			if w := runeLen(trimmed); w > maxR {
				maxR = w
			}
		}
	}

	art := maxR - minL
	if art <= 0 || art > SpriteCols {
		return frames // nothing to centre, or too wide to shift safely
	}

	shift := (SpriteCols-art)/2 - minL
	if shift == 0 {
		return frames
	}

	out := make([][]string, len(frames))
	for i, f := range frames {
		rows := make([]string, len(f))
		for r, row := range f {
			switch {
			case shift > 0:
				rows[r] = strings.Repeat(" ", shift) + row
			default:
				// Only ever trim leading spaces, never art.
				trim := -shift
				lead := runeLen(row) - runeLen(strings.TrimLeft(row, " "))
				if trim > lead {
					trim = lead
				}
				rows[r] = string([]rune(row)[trim:])
			}
			rows[r] = fitWidth(rows[r], SpriteCols)
		}
		out[i] = rows
	}
	return out
}

// fitToBox crops a species' art down to the BuddyRows-tall box, centring its
// ink vertically, and reports whether it fits at all.
//
// A species whose art needs more rows than the box is rejected outright. The
// alternative — cropping — would silently lop the antenna off the robot or the
// cap off the mushroom, and a subtly wrong sprite is worse than an absent one.
//
// As with horizontal centring, the window is chosen once across every frame so
// the creature cannot drift vertically as it animates.
func fitToBox(frames [][]string) ([][]string, bool) {
	first, last := SpriteRows, -1
	for _, f := range frames {
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
	if last < 0 {
		return frames, false
	}

	ink := last - first + 1
	if ink > BuddyRows {
		return nil, false
	}

	// Centre the ink in the box, then clamp so the window stays in range.
	top := first - (BuddyRows-ink)/2
	if top < 0 {
		top = 0
	}
	if top+BuddyRows > SpriteRows {
		top = SpriteRows - BuddyRows
	}

	blank := strings.Repeat(" ", SpriteCols)
	out := make([][]string, len(frames))
	for i, f := range frames {
		rows := make([]string, BuddyRows)
		for r := range rows {
			src := top + r
			if src < 0 || src >= len(f) {
				rows[r] = blank
				continue
			}
			rows[r] = f[src]
		}
		out[i] = rows
	}
	return out, true
}

// inkCenterRow is the row a species' art is visually centred on.
//
// For an even number of ink rows the true centre falls between two rows; this
// picks the upper one, because these creatures are top-heavy — the head is at
// the top and the feet or base at the bottom, so the visual weight sits above
// the geometric middle.
func inkCenterRow(frames [][]string) int {
	first, last := BuddyRows, -1
	for _, f := range frames {
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
	if last < 0 {
		return BuddyRows / 2
	}
	return first + (last-first)/2
}

func runeLen(s string) int { return len([]rune(s)) }

// fitWidth pads or trims a row to exactly width cells.
//
// Trimming is safe here only because centerFrames has already checked the art
// fits: shifting right pushes trailing spaces off the end, never glyphs.
func fitWidth(s string, width int) string {
	r := []rune(s)
	if len(r) > width {
		return string(r[:width])
	}
	return padRight(s, width)
}

func padRight(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// Face returns the buddy's rows for a frame, already styled.
func (b Buddy) Face(frame int, low bool) []string {
	pose := b.Frames[abs(frame/bodyHold)%len(b.Frames)]

	eye := eyeOpen
	style := styleValue
	switch {
	case low:
		// Alarmed eyes alternate faster than the body animation, so a buddy in
		// trouble reads as agitated rather than merely posed differently.
		eye = eyeAlarm
		if abs(frame/alarmHold)%2 == 1 {
			eye = "°"
		}
		style = styleAlarm
	case blinking(frame):
		eye = eyeShut
	}

	out := make([]string, len(pose))
	for i, row := range pose {
		out[i] = style.Render(strings.ReplaceAll(row, eyePlaceholder, eye))
	}
	return out
}

// BuddyNames lists every companion, for error messages and shell completion.
func BuddyNames() string {
	names := make([]string, len(Buddies))
	for i, b := range Buddies {
		names[i] = b.Name
	}
	return strings.Join(names, ", ")
}

// BuddyAt returns the buddy at an index, wrapping around.
func BuddyAt(i int) Buddy { return Buddies[mod(i, len(Buddies))] }

// BuddyIndexByName finds a buddy by name, returning -1 if there is no match.
// Used to resolve a saved default from config back into a cycle position.
func BuddyIndexByName(name string) int {
	for i, b := range Buddies {
		if b.Name == name {
			return i
		}
	}
	return -1
}

// MachineAt returns the machine at an index, wrapping around.
func MachineAt(i int) Machine { return Machines[mod(i, len(Machines))] }
