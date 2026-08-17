package install

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// preflight verifies the platform can run what was selected. On Apple
// Silicon that means Rosetta 2 for SteamCMD (the only Intel-only component;
// the game engines are universal binaries) — offered as a one-time install.
// On Linux, game downloads work on x86_64; engines are skipped elsewhere.
func (e *Engine) preflight(ctx context.Context) (string, error) {
	switch runtime.GOOS {
	case "darwin":
	case "linux":
		if e.needsSteam() && runtime.GOARCH != "amd64" {
			e.log(StepPreflight, "warning: SteamCMD is x86_64-only; on "+runtime.GOARCH+" it needs emulation (FEX/box64)")
		}
		return "", nil
	default:
		return "", fmt.Errorf("quakeup supports macOS and Linux (running on %s)", runtime.GOOS)
	}
	if !e.needsSteam() {
		return "SteamCMD not needed", nil
	}
	if runtime.GOARCH != "arm64" {
		return "", nil // Intel Mac: nothing to check
	}
	if exec.CommandContext(ctx, "arch", "-x86_64", "/usr/bin/true").Run() == nil {
		return "", nil // Rosetta present
	}

	e.log(StepPreflight, "SteamCMD is an Intel binary and needs Rosetta 2 (one-time install).")
	answer, err := e.ask(ctx, StepPreflight, "Install Rosetta 2 now? [y/N]", false)
	if err != nil {
		return "", err
	}
	if s := strings.ToLower(strings.TrimSpace(answer)); s != "y" && s != "yes" {
		return "", fmt.Errorf("Rosetta 2 is required to run SteamCMD; re-run quakeup when ready")
	}
	e.log(StepPreflight, "Installing Rosetta 2…")
	out, err := exec.CommandContext(ctx, "softwareupdate", "--install-rosetta", "--agree-to-license").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Rosetta 2 install failed: %v: %s", err, firstLine(out))
	}
	return "", nil
}

// needsSteam reports whether this run will invoke SteamCMD at all.
func (e *Engine) needsSteam() bool {
	return !e.opts.EnginesOnly && (e.opts.NeedsQuake1Data() || e.opts.Quake3)
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
