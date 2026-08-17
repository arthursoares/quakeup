// quakeup sets up Quake (2021 rerelease) and Quake III Arena on a Mac:
// it downloads the game data from Steam with the user's own account via
// SteamCMD, installs the native vkQuake and ioquake3 engines, and wires
// everything together.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/arthursoares/quakeup/internal/install"
	"github.com/arthursoares/quakeup/internal/ui"
)

// Set via -ldflags at release time.
var version = "dev"

func main() {
	var opts install.Options
	var plain, showVersion bool
	flag.StringVar(&opts.Dir, "dir", "", "install directory (default ~/Games/Quake)")
	flag.StringVar(&opts.AppsDir, "apps-dir", "", "directory for .app bundles (default /Applications)")
	flag.StringVar(&opts.SteamUser, "user", "", "Steam account name (prompted when needed if unset)")
	flag.BoolVar(&opts.EnginesOnly, "engines-only", false, "only install/repair the game engines")
	flag.BoolVar(&opts.GamesOnly, "games-only", false, "only download the game data")
	flag.BoolVar(&plain, "plain", false, "plain log output instead of the interactive UI")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println("quakeup", version)
		return
	}
	if opts.EnginesOnly && opts.GamesOnly {
		fmt.Fprintln(os.Stderr, "--engines-only and --games-only are mutually exclusive")
		os.Exit(2)
	}

	engine, err := install.New(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "quakeup:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		<-sig
		cancel()
	}()

	go engine.Run(ctx)

	ok := false
	if plain || !term.IsTerminal(int(os.Stdout.Fd())) {
		ok = runPlain(engine)
	} else {
		ok = runTUI(engine, cancel)
	}
	if !ok {
		os.Exit(1)
	}
}

func runTUI(engine *install.Engine, cancel context.CancelFunc) bool {
	m := ui.New(engine.Events(), cancel, engine.Options())
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "quakeup:", err)
		return false
	}
	if fm, isModel := final.(ui.Model); isModel {
		return fm.Done() && !fm.Failed()
	}
	return false
}

func runPlain(engine *install.Engine) bool {
	stdin := bufio.NewReader(os.Stdin)
	succeeded := false
	for ev := range engine.Events() {
		switch ev.Kind {
		case install.EvStepStart:
			fmt.Printf("==> %s\n", ev.Step.Title())
		case install.EvStepDone:
			fmt.Printf("    done %s\n", ev.Msg)
		case install.EvStepSkip:
			fmt.Printf("    skipped: %s\n", ev.Msg)
		case install.EvProgress:
			if ev.Pct >= 0 {
				fmt.Printf("    %s %.1f%% (%d / %d)\n", ev.Msg, ev.Pct*100, ev.BytesDone, ev.BytesTotal)
			}
		case install.EvLog:
			fmt.Printf("    %s\n", ev.Msg)
		case install.EvNeedInput:
			fmt.Printf("%s: ", ev.Msg)
			var line string
			if ev.Masked && term.IsTerminal(int(os.Stdin.Fd())) {
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println()
				if err == nil {
					line = string(b)
				}
			} else {
				line, _ = stdin.ReadString('\n')
			}
			ev.Reply <- strings.TrimRight(line, "\r\n")
		case install.EvFatal:
			fmt.Fprintf(os.Stderr, "ERROR in %s: %s\n", ev.Step.Title(), ev.Msg)
		case install.EvDone:
			succeeded = true
			opts := engine.Options()
			fmt.Println("\nReady to play!")
			fmt.Printf("  Quake 1:          %s/play-quake1.sh\n", opts.Dir)
			fmt.Printf("  Quake III Arena:  %s/play-quake3.sh\n", opts.Dir)
		}
	}
	return succeeded
}
