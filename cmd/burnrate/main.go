// Command burnrate shows your Cursor usage as an arcade HUD.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/andregcab/burnrate/internal/config"
	"github.com/andregcab/burnrate/internal/cursor"
	"github.com/andregcab/burnrate/internal/provider"
	"github.com/andregcab/burnrate/internal/stats"
	"github.com/andregcab/burnrate/internal/ui"
)

func main() {
	var (
		asJSON    = flag.Bool("json", false, "print the snapshot as JSON and exit")
		once      = flag.Bool("once", false, "print the HUD once and exit (no TUI)")
		demo      = flag.Bool("demo", false, "run on synthetic data — no credentials, no API calls")
		drain     = flag.Duration("drain", 0, "with --demo, drain the HP bar over this long (e.g. 30s)")
		demoHP    = flag.Float64("demo-hp", -1, "with --demo, force the HP fraction 0..1 (for checking the alarm state)")
		topN      = flag.Int("top", 5, "how many models to show")
		verify    = flag.Bool("verify", false, "with --once, cross-check aggregation against the authoritative total")
		machine   machineFlag
		noMachine = flag.Bool("no-machine", false, "hide the machine section")
		buddy     = flag.String("buddy", "", "which companion by name (overrides the saved default)")
	)
	flag.Var(&machine, "machine",
		"show the machine section; optionally pick one with -machine=<slug> "+
			"(money-furnace, token-factory, reactor-core, pumpjack)")
	flag.Parse()

	// -machine behaves like a bool so that a bare -machine works, which means
	// the space form -machine <slug> would silently drop the slug as a
	// positional argument. Catching it here turns a confusing no-op into a
	// message that says exactly what to type.
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr,
			"burnrate: unexpected argument %q\n         did you mean -machine=%s ?\n",
			flag.Arg(0), flag.Arg(0))
		os.Exit(2)
	}

	if err := run(opts{
		asJSON: *asJSON, once: *once, demo: *demo,
		drain: *drain, topN: *topN, verify: *verify, demoHP: *demoHP, machine: machine.name, showMachine: machine.set,
		noMachine: *noMachine, buddy: *buddy,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "burnrate: %v\n", err)
		os.Exit(1)
	}
}

// machineFlag lets -machine work both bare and with a value.
//
// Go's flag package requires a value for anything that is not a bool, so a
// plain -machine would error. Implementing IsBoolFlag makes the bare form
// legal while -machine=<slug> still selects a specific one.
type machineFlag struct {
	set  bool
	name string
}

func (m *machineFlag) String() string { return m.name }

func (m *machineFlag) Set(v string) error {
	m.set = true
	// The flag package passes "true" for the bare form.
	if v != "true" {
		m.name = v
	}
	return nil
}

// IsBoolFlag tells the flag package a bare -machine is valid.
func (m *machineFlag) IsBoolFlag() bool { return true }

type opts struct {
	asJSON, once, demo, verify bool
	noMachine, showMachine     bool
	machine                    string
	buddy                      string
	drain                      time.Duration
	topN                       int
	demoHP                     float64
}

func run(o opts) error {
	prov, refresh, err := buildProvider(o)
	if err != nil {
		return err
	}

	look, err := resolveLook(o)
	if err != nil {
		return err
	}

	// One-shot modes render and exit, for scripting and for verification.
	if o.asJSON || o.once {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		snap, err := prov.Snapshot(ctx)
		if err != nil {
			return err
		}
		if o.asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(snap)
		}
		fmt.Println(ui.Render(snap, time.Now(), 0, ui.Opts{
			Arcade: look.machineOn, HP: -1, CoinAge: -1,
			Machine: look.machine, Buddy: look.buddy, Legend: true,
		}))
		if o.verify {
			printVerification(snap)
		}
		return nil
	}

	model := ui.NewModel(prov, refresh, look.machineOn, look.machine, look.buddy)
	if !o.demo {
		// --demo is a throwaway session and must not rewrite config.
		model = model.SetSaveLook(config.SetLook)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// look is the resolved arrangement: which companion, which machine, and
// whether the machine is showing.
type look struct {
	buddy     int
	machine   int
	machineOn bool
}

// resolveLook applies flags over the saved default over the catalog's first
// entry. Unknown names are reported rather than silently ignored — a typo that
// quietly does nothing is worse than an error.
func resolveLook(o opts) (look, error) {
	var l look

	// Saved defaults first, so flags can override them.
	if !o.demo {
		if cfg, err := config.Load(); err == nil {
			if i := ui.BuddyIndexByName(cfg.Buddy); i >= 0 {
				l.buddy = i
			}
			if i := ui.MachineIndexBySlug(cfg.Machine); i >= 0 {
				l.machine = i
			}
			l.machineOn = cfg.MachineOn
		}
	}

	if o.buddy != "" {
		i := ui.BuddyIndexByName(o.buddy)
		if i < 0 {
			return l, fmt.Errorf("unknown buddy %q; try one of: %s", o.buddy, ui.BuddyNames())
		}
		l.buddy = i
	}

	// A bare -machine means "show it", whichever one is current.
	if o.showMachine {
		l.machineOn = true
	}

	if o.machine != "" {
		i := ui.MachineIndexBySlug(o.machine)
		if i < 0 {
			return l, fmt.Errorf("unknown machine %q; try one of: %s", o.machine, ui.MachineSlugs())
		}
		l.machine = i
		// Naming a machine means you want to see it.
		l.machineOn = true
	}

	// An explicit --no-machine beats everything, including a saved look.
	if o.noMachine {
		l.machineOn = false
	}

	return l, nil
}

func buildProvider(o opts) (provider.Provider, time.Duration, error) {
	if o.demo {
		snap := provider.DemoSnapshot(time.Now())
		if o.demoHP >= 0 && o.demoHP <= 1 && snap.LimitCents > 0 {
			snap.FractionLeft = o.demoHP
			snap.RemainingCents = int(float64(snap.LimitCents) * o.demoHP)
			snap.SpentCents = snap.LimitCents - snap.RemainingCents
		}
		return &provider.Static{Snap: snap, Drain: o.drain}, time.Second, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, 0, err
	}

	cookie, err := config.SessionCookie()
	if err != nil {
		return nil, 0, err
	}

	teamID := cfg.TeamID
	if teamID == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		acct, err := cursor.New(cookie, 0).Account(ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("discovering team id: %w", err)
		}
		teamID = acct.TeamID
		if teamID == 0 {
			return nil, 0, errors.New("no team id on this account; the usage-events endpoint requires one")
		}
	}

	return provider.NewSession(cursor.New(cookie, teamID), o.topN), cfg.Refresh(), nil
}

func printVerification(s stats.Snapshot) {
	fmt.Printf("\n  verification\n")
	fmt.Printf("    summed from events : $%.2f\n", s.ModelSpendCents/100)
	fmt.Printf("    authoritative      : $%.2f\n", float64(s.SpentCents)/100)
	if s.SpentCents == 0 {
		return
	}
	drift := (s.ModelSpendCents - float64(s.SpentCents)) / float64(s.SpentCents) * 100
	verdict := "OK — within tolerance"
	if drift > 5 || drift < -5 {
		verdict = "MISMATCH — aggregation is wrong"
	}
	fmt.Printf("    drift              : %+.2f%%  (%s)\n", drift, verdict)
}
