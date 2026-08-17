package install

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// preflight verifies we are on macOS and, on Apple Silicon, that Rosetta 2 is
// available for SteamCMD (the only Intel-only component; both game engines
// are universal binaries). Offers to install Rosetta when missing.
func (e *Engine) preflight(ctx context.Context) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("quakeup only supports macOS (running on %s)", runtime.GOOS)
	}
	if e.opts.EnginesOnly {
		return "engines only; SteamCMD not needed", nil
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

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
