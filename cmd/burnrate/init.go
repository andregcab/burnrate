package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/andregcab/burnrate/internal/config"
	"github.com/andregcab/burnrate/internal/cursor"
)

// runInit walks a first-time setup.
//
// Everything it needs is discoverable except the session cookie, so that is the
// only thing it asks for. The email and team id come from the API, which avoids
// the two most likely setup mistakes: a typo'd address, and hunting for a team
// id in a dashboard URL.
//
// It verifies before it writes. Storing an unverified credential and failing on
// first run is a far worse experience than failing here, where the error can
// still explain itself.
func runInit() error {
	fmt.Println()
	fmt.Println("  burnrate setup")
	fmt.Println()
	fmt.Println("  burnrate reads your usage from the Cursor dashboard, which")
	fmt.Println("  authenticates with your browser session — not an API key.")
	fmt.Println("  (Cursor's documented Admin API needs a team-admin key that")
	fmt.Println("  ordinary members can't create; see probe/FINDINGS.md.)")
	fmt.Println()
	fmt.Println("  To find it:")
	fmt.Println("    1. open https://cursor.com/dashboard while logged in")
	fmt.Println("    2. DevTools (Cmd-Opt-I) > Application > Cookies > cursor.com")
	fmt.Println("    3. copy the full value of  WorkosCursorSessionToken")
	fmt.Println()
	fmt.Println("  That cookie is your logged-in session: treat it like a password.")
	fmt.Println("  It is stored in your macOS Keychain, never in a file.")
	fmt.Println()

	cookie, err := promptSecret("  paste the cookie: ")
	if err != nil {
		return err
	}
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return fmt.Errorf("no cookie entered")
	}

	// Verify before storing.
	fmt.Print("\n  checking… ")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := cursor.New(cookie, 0)
	acct, err := client.Account(ctx)
	if err != nil {
		fmt.Println("failed")
		return fmt.Errorf("that cookie did not work: %w", err)
	}
	if acct.TeamID == 0 {
		fmt.Println("failed")
		return fmt.Errorf("this account has no team; burnrate needs one to read usage events")
	}

	sum, err := cursor.New(cookie, acct.TeamID).UsageSummary(ctx)
	if err != nil {
		fmt.Println("failed")
		return fmt.Errorf("could not read usage: %w", err)
	}
	fmt.Println("ok")

	if err := config.SetSessionCookie(cookie); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.TeamID = acct.TeamID
	if err := cfg.Save(); err != nil {
		return err
	}

	b := sum.IndividualUsage.Overall
	fmt.Println()
	fmt.Printf("  team      %d (%s)\n", acct.TeamID, acct.MembershipType)
	fmt.Printf("  budget    $%.2f of $%.2f used this cycle\n",
		float64(b.Used)/100, float64(b.Limit)/100)
	fmt.Printf("  resets    %s\n", sum.BillingCycleEnd.Local().Format("Jan 2"))

	if exp, err := cursor.CookieExpiry(cookie); err == nil {
		days := int(time.Until(exp).Hours() / 24)
		fmt.Printf("  cookie    valid for %d more days (until %s)\n",
			days, exp.Local().Format("Jan 2"))
		fmt.Println()
		fmt.Println("  Cursor sessions can't be renewed programmatically, so when")
		fmt.Println("  that lapses just run `burnrate init` again. burnrate will")
		fmt.Println("  start reminding you a week ahead.")
	}

	fmt.Println()
	fmt.Println("  done — run `burnrate` to start")
	fmt.Println()
	return nil
}

// promptSecret reads a line without echoing it.
//
// Falls back to a plain read when stdin is not a terminal, so the setup can
// still be scripted or piped.
func promptSecret(prompt string) (string, error) {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.TrimSpace(line), err
	}

	b, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return string(b), nil
}
