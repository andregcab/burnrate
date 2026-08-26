// Package ui renders a stats.Snapshot as a cyberpunk arcade HUD.
package ui

import "github.com/charmbracelet/lipgloss"

// The palette is a cool cyberpunk wash: near-white for figures, teal for
// labels and chrome, amber for money, red for alarm.
//
// Everything here is deliberately brighter than a conventional "dim" terminal
// scheme. A HUD that sits in peripheral vision has to stay readable at a
// glance, and low-contrast grey fails exactly when you most want to skim it.
// Nothing drops below roughly 55% luminance against a dark background.
var (
	// bright is the headline: the big dollar figure and the title.
	bright = lipgloss.AdaptiveColor{Light: "#0B0F1A", Dark: "#EDF3FF"}

	// white carries ordinary values.
	white = lipgloss.AdaptiveColor{Light: "#1A2233", Dark: "#D3DCEF"}

	// label is the teal used for field names and gauges.
	label = lipgloss.AdaptiveColor{Light: "#0F6F6C", Dark: "#5FD3C4"}

	// dim draws the cabinet chrome. Tinted rather than neutral, so the frame
	// reads as part of the design instead of as washed-out text.
	dim = lipgloss.AdaptiveColor{Light: "#4A6572", Dark: "#6E8CA6"}

	// faint is secondary text. Previously near-invisible at #3A3A3A; now a
	// readable blue-grey.
	faint = lipgloss.AdaptiveColor{Light: "#5B6B80", Dark: "#94A3BF"}

	// money is the amber used for costs, the one warm note in the scheme.
	money_ = lipgloss.AdaptiveColor{Light: "#8A6A00", Dark: "#E9C46A"}

	// rainC is the falling-glyph backdrop: present, never competing with text.
	rainC = lipgloss.AdaptiveColor{Light: "#9FBAC4", Dark: "#2F5C63"}

	// track is the unfilled part of a gauge: visible as a rail, never
	// competing with the filled portion.
	track = lipgloss.AdaptiveColor{Light: "#A9B6C9", Dark: "#4A5D78"}

	// flameTip and flameBase colour the wordmark's fire.
	flameTip  = lipgloss.AdaptiveColor{Light: "#C2410C", Dark: "#FF9E4A"}
	flameBase = lipgloss.AdaptiveColor{Light: "#9A3412", Dark: "#B45309"}

	// alarm is low budget.
	alarm = lipgloss.AdaptiveColor{Light: "#B00020", Dark: "#FF6B6B"}
)

var (
	styleTitle  = lipgloss.NewStyle().Foreground(bright).Bold(true)
	styleLabel  = lipgloss.NewStyle().Foreground(label)
	styleValue  = lipgloss.NewStyle().Foreground(white).Bold(true)
	styleFigure = lipgloss.NewStyle().Foreground(bright).Bold(true)
	styleFaint  = lipgloss.NewStyle().Foreground(faint)
	styleBorder = lipgloss.NewStyle().Foreground(dim)
	styleMoney  = lipgloss.NewStyle().Foreground(money_)
	styleRain   = lipgloss.NewStyle().Foreground(rainC)
	styleAlarm  = lipgloss.NewStyle().Foreground(alarm).Bold(true)

	// styleTrack is the unfilled half of a gauge.
	styleTrack = lipgloss.NewStyle().Foreground(track)

	// Gauge fills are deliberately NOT bold.
	//
	// Terminals render bold by thickening the glyph, and on a dither pattern
	// that makes the filled run visibly heavier and taller than the track
	// beside it — the two halves of one bar end up at different heights. The
	// model bars never had this because they were already drawn unbolded.
	styleBarFill  = lipgloss.NewStyle().Foreground(bright)
	styleBarAlarm = lipgloss.NewStyle().Foreground(alarm)
	styleWarn     = lipgloss.NewStyle().Foreground(alarm)
	styleCoin     = lipgloss.NewStyle().Foreground(money_).Bold(true)

	// Fire runs hot at the tips and cools toward the base. Three block
	// characters only read as flame if they are not one flat colour.
	styleFlameTip  = lipgloss.NewStyle().Foreground(flameTip)
	styleFlameBase = lipgloss.NewStyle().Foreground(flameBase)
)

// Block characters for the gauges. Half blocks double the horizontal
// resolution, which matters on a 38-cell bar.
const (
	// Both halves of a gauge are dither patterns, never a full block.
	//
	// █ fills its entire line box, leading included, so stacked filled runs fuse
	// into one continuous shape while the shaded tails beside them keep a clean
	// gap between rows. That mismatch is what made the bars look wrong, and no
	// amount of row padding fixes it — a terminal row is atomic, so the choice
	// was one blank line or none, and neither matched.
	//
	// ▓ and ░ are drawn as dot patterns that stop short of the cell's top and
	// bottom edges. Using both means every row carries the same built-in gap,
	// so the list breathes without spending a single extra line.
	//
	// There is deliberately no third, mid-density glyph for partial cells. It
	// doubled the gauge's resolution but nobody could tell what it meant at a
	// glance, and an unreadable detail is worse than a coarser bar.
	blockFull  = "▓"
	blockEmpty = "░"
)

// lowHP is the fraction below which the HUD switches to its alarm state.
const lowHP = 0.20
