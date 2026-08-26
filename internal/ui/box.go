package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Cabinet chrome.
//
// Lip Gloss borders are a single static rectangle, but the HUD needs labelled
// section dividers and a border that animates, so the frame is drawn by hand.
// Every helper here returns a line of exactly Width cells so the cabinet cannot
// go ragged as content changes.

const (
	cTL, cTR, cBL, cBR = "╔", "╗", "╚", "╝"
	cH, cV             = "═", "║"
	cDivL, cDivR       = "╟", "╢"
	cDivH              = "─"
)

// Top renders the cabinet's top edge.
func Top(width int) string {
	return styleBorder.Render(cTL + strings.Repeat(cH, width-2) + cTR)
}

// Bottom renders the cabinet's bottom edge.
func Bottom(width int) string {
	return styleBorder.Render(cBL + strings.Repeat(cH, width-2) + cBR)
}

// Divider renders a labelled section rule: ╟─ LABEL ────── RIGHT ─╢
func Divider(width int, label, right string) string {
	inner := width - 2

	var b strings.Builder
	b.WriteString(cDivL)

	if label != "" {
		lab := " " + label + " "
		b.WriteString(cDivH)
		b.WriteString(lab)
		inner -= 1 + lipgloss.Width(lab)
	}

	if right != "" {
		r := " " + right + " "
		fill := inner - lipgloss.Width(r) - 1
		if fill < 1 {
			fill = 1
		}
		b.WriteString(strings.Repeat(cDivH, fill))
		b.WriteString(r)
		b.WriteString(cDivH)
	} else {
		if inner < 1 {
			inner = 1
		}
		b.WriteString(strings.Repeat(cDivH, inner))
	}

	b.WriteString(cDivR)

	// The label and count read as chrome, not data, so they stay dim.
	return styleBorder.Render(b.String())
}

// Row wraps one line of content in the cabinet's side walls, inset by a gutter
// on each side.
//
// Padding is computed on display width, not byte or rune count: block glyphs
// and box-drawing characters are not all one cell wide, and padding by len()
// leaves the right wall ragged at one specific value.
func Row(content string, width, gutter int) string {
	inset := strings.Repeat(" ", gutter)
	pad := width - 2 - gutter*2 - lipgloss.Width(content)
	if pad < 0 {
		pad = 0
	}
	return styleBorder.Render(cV) + inset + content + strings.Repeat(" ", pad) +
		inset + styleBorder.Render(cV)
}

func mod(a, m int) int {
	r := a % m
	if r < 0 {
		r += m
	}
	return r
}
