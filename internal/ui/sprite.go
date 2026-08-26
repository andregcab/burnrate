package ui

import "strings"

// Sprite art and idle animation.
//
// Every animation derives from a single monotonically increasing frame counter
// rather than wall-clock time, so the same frame always produces the same art.
// That keeps rendering deterministic and testable.

const (
	fps = 8

	// blinkFrames is how long the eyes stay shut. A real blink is about 150ms,
	// so two frames at 8fps is right: the blink itself should look normal, it
	// is the spacing between blinks that should be long.
	blinkFrames = 2

	// Blinks are spaced randomly between these bounds, in seconds, so the face
	// never falls into a metronome rhythm. A perfectly regular blink reads as a
	// loading spinner rather than as something alive.
	blinkGapMinSec = 4
	blinkGapMaxSec = 6

	// alarmHold: faster than everything else, because low budget should feel
	// urgent.
	alarmHold = 5

	// coinHold: the spend-pop spinner advances a quarter turn per second.
	coinHold = 8
)

// blinkStarts holds one cycle's worth of blink onsets, in frames. The schedule
// is precomputed so lookup is O(log n) at any frame, however long the program
// runs, while still looking irregular.
var blinkStarts []int
var blinkCycle int

func init() {
	// A small deterministic PRNG. Seeded by a constant so the schedule is
	// identical every run and tests stay reproducible; irregular enough that
	// the eye reads it as random.
	seed := uint32(0x9E3779B9)
	next := func() uint32 {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		return seed
	}

	// Twenty blinks is about a hundred seconds of cycle, far longer than anyone
	// watches closely enough to notice it repeat.
	at := 0
	for i := 0; i < 20; i++ {
		span := blinkGapMaxSec - blinkGapMinSec + 1
		gap := (blinkGapMinSec + int(next()%uint32(span))) * fps
		at += gap
		blinkStarts = append(blinkStarts, at)
	}
	blinkCycle = at + blinkGapMinSec*fps
}

// blinking reports whether the eyes are shut on this frame.
func blinking(frame int) bool {
	f := mod(frame, blinkCycle)
	for _, start := range blinkStarts {
		if f >= start && f < start+blinkFrames {
			return true
		}
		if start > f {
			break // schedule is sorted, so nothing later can match
		}
	}
	return false
}

// coinFrames is the liveness tell during a spend pop.
var coinFrames = []string{"◐", "◓", "◑", "◒"}

// Coin returns the spinner glyph for a frame.
func Coin(frame int) string { return coinFrames[abs(frame/coinHold)%len(coinFrames)] }

// pipFrames fade a spend-pop out, so it draws the eye briefly and then stops
// competing with the numbers.
var pipFrames = []string{"✦", "✦", "✧", "✧", "·", "·", " "}

// Pip returns the coin-pop glyph for a given age in frames.
func Pip(age int) string {
	i := age / 2
	if age < 0 || i >= len(pipFrames) {
		return " "
	}
	return pipFrames[i]
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

// The title flame.
//
// A small fire burning beside the wordmark. It flickers faster than anything
// else in the HUD — around 0.6s per frame — because fire that moves at the same
// stately pace as a blinking pet reads as a lava lamp rather than a flame.
const flameHold = 5

// flameFrames are heights per cell, 0 (nothing) to 6 (tallest). Five frames, an
// odd count, so the flicker does not fall into an obvious repeating rhythm.
var flameFrames = [][]int{
	{2, 5, 3},
	{3, 6, 2},
	{1, 4, 4},
	{3, 5, 1},
	{2, 6, 3},
}

// flameGlyphs index by height.
var flameGlyphs = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆"}

// Flame renders the wordmark's fire for a frame.
//
// Each cell is coloured by its own height: the tips run hot and the base sits
// amber, which is what makes three block characters read as fire rather than as
// a bar chart.
func Flame(frame int) string {
	heights := flameFrames[abs(frame/flameHold)%len(flameFrames)]

	var b strings.Builder
	for _, h := range heights {
		if h < 0 {
			h = 0
		}
		if h >= len(flameGlyphs) {
			h = len(flameGlyphs) - 1
		}
		style := styleMoney
		if h >= 5 {
			style = styleFlameTip
		} else if h <= 1 {
			style = styleFlameBase
		}
		b.WriteString(style.Render(flameGlyphs[h]))
	}
	return b.String()
}
