package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"

	"github.com/andregcab/burnrate/internal/stats"
)

// Fetcher returns a fresh snapshot. The UI depends on this and never on the API
// client, so the data source can be swapped without the renderer changing.
type Fetcher interface {
	Snapshot(ctx context.Context) (stats.Snapshot, error)
}

// tickInterval drives the animation. ~8fps is enough for a blink to read as a
// blink while costing essentially nothing.
const tickInterval = 125 * time.Millisecond

// coinLifetime is how many frames a spend-pop stays on screen.
const coinLifetime = 4

type tickMsg time.Time

type snapshotMsg struct {
	snap stats.Snapshot
	err  error
}

// Model is the Bubble Tea model for the HUD.
type Model struct {
	fetcher Fetcher
	refresh time.Duration
	arcade  bool

	snap  stats.Snapshot
	err   error
	frame int

	// The HP bar is spring-driven so it glides to a new value instead of
	// snapping. Springs also overshoot slightly, which makes a drop feel like
	// damage taken rather than a number being replaced.
	spring   harmonica.Spring
	hpPos    float64
	hpVel    float64
	hpTarget float64
	primed   bool

	// coinAge counts frames since spend last increased; -1 means no pop.
	coinAge   int
	coinCents float64
	lastSpend int

	machine int
	buddy   int
	legend  bool

	// expiry is a standing warning about the session cookie, shown when it is
	// close to lapsing. Empty when there is nothing to say.
	expiry string

	// saveLook persists the current arrangement. Nil disables the `s` key,
	// which is what --demo wants: a throwaway session should not rewrite config.
	saveLook   func(buddy, machine string, machineOn bool) error
	savedMsg   string
	savedUntil int

	lastFetch time.Time
	loading   bool
	quitting  bool
}

// NewModel builds the HUD model.
func NewModel(f Fetcher, refresh time.Duration, arcade bool, machine, buddy int) Model {
	return Model{
		fetcher: f,
		refresh: refresh,
		arcade:  arcade,
		machine: machine,
		buddy:   buddy,
		loading: true,
		legend:  true,
		coinAge: -1,
		// Tuned by eye at 8fps: firm enough to settle within a second, loose
		// enough to visibly travel.
		spring: harmonica.NewSpring(harmonica.FPS(8), 6.0, 0.45),
	}
}

// Init starts the ticker and the first fetch.
func (m Model) Init() tea.Cmd { return tea.Batch(tick(), m.fetch()) }

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		snap, err := m.fetcher.Snapshot(ctx)
		return snapshotMsg{snap: snap, err: err}
	}
}

// Update handles keys, animation ticks, and fetch results.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				return m, m.fetch()
			}
		case "a":
			m.arcade = !m.arcade
		case "m":
			// Turning the machine on is the obvious intent of pressing the
			// machine key while it is hidden, so do that before cycling.
			if !m.arcade {
				m.arcade = true
			} else {
				m.machine++
			}
		case "b":
			m.buddy++
		case "s":
			if m.saveLook != nil {
				buddy := BuddyAt(m.buddy).Name
				machine := MachineSlug(MachineAt(m.machine))
				if err := m.saveLook(buddy, machine, m.arcade); err != nil {
					m.savedMsg = "save failed"
				} else {
					m.savedMsg = "saved " + buddy + " + " + machine
				}
				// Roughly four seconds at the tick rate.
				m.savedUntil = m.frame + 4*fps
			}
		case "l", "?":
			m.legend = !m.legend
		}

	case tickMsg:
		m.frame++
		if m.savedMsg != "" && m.frame >= m.savedUntil {
			m.savedMsg = ""
		}
		m.hpPos, m.hpVel = m.spring.Update(m.hpPos, m.hpVel, m.hpTarget)

		if m.coinAge >= 0 {
			m.coinAge++
			if m.coinAge >= coinLifetime {
				m.coinAge = -1
			}
		}

		// Poll on the refresh interval off the same clock, so the model has
		// exactly one timer rather than two that can drift apart.
		if !m.loading && time.Since(m.lastFetch) >= m.refresh {
			m.loading = true
			return m, tea.Batch(tick(), m.fetch())
		}
		return m, tick()

	case snapshotMsg:
		m.loading = false
		m.lastFetch = time.Now()

		if msg.err != nil {
			m.err = msg.err
			// Keep the previous numbers but flag them stale. Blanking the HUD
			// on a transient blip would be worse than showing a stale marker.
			m.snap.Stale = true
			return m, nil
		}

		if m.primed && msg.snap.SpentCents > m.lastSpend {
			m.coinCents = float64(msg.snap.SpentCents - m.lastSpend)
			m.coinAge = 0
		}
		m.lastSpend = msg.snap.SpentCents

		m.err = nil
		m.snap = msg.snap
		m.hpTarget = msg.snap.FractionLeft

		// Start the bar at its true value on the first load. Animating up from
		// zero on launch would imply a change that did not happen.
		if !m.primed {
			m.hpPos = msg.snap.FractionLeft
			m.primed = true
		}
		return m, nil
	}

	return m, nil
}

// View renders the HUD.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	if m.err != nil && m.snap.FetchedAt.IsZero() {
		// Nothing ever loaded, so there is no HUD to degrade to.
		return Panel(m.frame, false,
			styleTitle.Render("BURNRATE"),
			"",
			styleAlarm.Render("✖ "+firstLine(m.err.Error())),
			wrapDim(m.err.Error(), Width-6),
			"",
			styleFaint.Render("r retry · q quit"))
	}

	if m.snap.FetchedAt.IsZero() {
		return Panel(m.frame, m.arcade,
			styleTitle.Render("BURNRATE"),
			"",
			styleLabel.Render(Coin(m.frame)+" reading usage…"))
	}

	out := Render(m.snap, time.Now(), m.frame, Opts{
		Arcade:    m.arcade,
		HP:        m.hpPos,
		CoinAge:   m.coinAge,
		CoinCents: m.coinCents,
		Machine:   m.machine,
		Buddy:     m.buddy,
		Saved:     m.saved(),
		Notice:    m.expiry,
		Legend:    m.legend,
	})

	if m.err != nil {
		out += "\n" + styleAlarm.Render("  refresh failed: "+m.err.Error())
	}
	return out
}

// saved returns the transient save confirmation, if one is still showing.
func (m Model) saved() string { return m.savedMsg }

// SetInitialSnapshot seeds the model with cached figures so the HUD has
// something to draw before the first fetch returns. The snapshot arrives
// already marked stale, and is replaced as soon as live data lands.
func (m Model) SetInitialSnapshot(s stats.Snapshot) Model {
	m.snap = s
	m.hpPos = s.FractionLeft
	m.hpTarget = s.FractionLeft
	m.lastSpend = s.SpentCents
	m.primed = true
	return m
}

// SetExpiryWarning installs a standing notice about the session cookie.
func (m Model) SetExpiryWarning(msg string) Model {
	m.expiry = msg
	return m
}

// SetSaveLook installs the callback used by the `s` key.
func (m Model) SetSaveLook(fn func(buddy, machine string, machineOn bool) error) Model {
	m.saveLook = fn
	return m
}

// Panel renders a simple bordered card, for states with no HUD to show.
func Panel(frame int, arcade bool, lines ...string) string {
	_ = frame
	_ = arcade
	out := []string{Top(Width), Row("", Width, gutter)}
	for _, ln := range lines {
		for _, part := range strings.Split(ln, "\n") {
			out = append(out, Row(part, Width, gutter))
		}
	}
	out = append(out, Row("", Width, gutter))
	return strings.Join(append(out, Bottom(Width)), "\n")
}

// firstLine is the headline of a multi-clause error, so the panel leads with
// the actionable part rather than the wrapped detail.
func firstLine(s string) string {
	if i := strings.Index(s, ":"); i > 0 && i < 60 {
		return s[:i]
	}
	if len(s) > 56 {
		return s[:56]
	}
	return s
}

// wrapDim word-wraps error detail to fit inside the cabinet.
func wrapDim(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	lines = append(lines, cur)
	for i := range lines {
		lines[i] = styleFaint.Render(lines[i])
	}
	return strings.Join(lines, "\n")
}
