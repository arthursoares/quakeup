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
	flag.BoolVar(&opts.Quake1, "quake1", false, "install Quake (2021 rerelease) + vkQuake")
	flag.BoolVar(&opts.Quake3, "quake3", false, "install Quake III Arena + ioquake3")
	flag.BoolVar(&opts.EzQuake, "ezquake", false, "install ezQuake (QuakeWorld deathmatch client)")
	flag.BoolVar(&opts.ServerFiles, "server-files", false, "generate docker-compose files for self-hosted servers")
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

	// No explicit selection: show the picker in the TUI, or default to both
	// games (the pre-selection behavior) when headless.
	if !opts.Quake1 && !opts.Quake3 && !opts.EzQuake && !opts.ServerFiles {
		if !plain && term.IsTerminal(int(os.Stdout.Fd())) {
			final, err := tea.NewProgram(ui.NewSelection()).Run()
			if err != nil {
				fmt.Fprintln(os.Stderr, "quakeup:", err)
				os.Exit(1)
			}
			if sel, isSel := final.(ui.Selection); !isSel || !sel.Apply(&opts) {
				return // user backed out
			}
		} else {
			opts.Quake1, opts.Quake3 = true, true
		}
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
			fmt.Println("\nDone!")
			for _, l := range []struct{ label, script string }{
				{"Quake 1:         ", "play-quake1.sh"},
				{"QuakeWorld:      ", "play-quakeworld.sh"},
				{"Quake III Arena: ", "play-quake3.sh"},
			} {
				path := opts.Dir + "/" + l.script
				if _, err := os.Stat(path); err == nil {
					fmt.Printf("  %s %s\n", l.label, path)
				}
			}
			if _, err := os.Stat(opts.Dir + "/server/docker-compose.yml"); err == nil {
				fmt.Printf("  Servers:          edit %s/server/.env, then docker compose up -d\n", opts.Dir)
			}
		}
	}
	return succeeded
}
