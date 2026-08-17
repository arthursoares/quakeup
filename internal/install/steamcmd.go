package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/creack/pty"
)

func (e *Engine) steamcmdDir() string { return filepath.Join(e.opts.Dir, "steamcmd") }

// ensureSteamCMD downloads and extracts SteamCMD into <dir>/steamcmd.
// Extraction is staged into a temp directory and renamed so an interrupted
// run is never mistaken for a working install.
func (e *Engine) ensureSteamCMD(ctx context.Context) (string, error) {
	if e.opts.EnginesOnly {
		return "engines only", nil
	}
	if !e.opts.NeedsQuake1Data() && !e.opts.Quake3 {
		return "no game downloads selected", nil
	}
	script := filepath.Join(e.steamcmdDir(), "steamcmd.sh")
	if fileExecutable(script) {
		return "already installed", nil
	}
	if err := os.MkdirAll(e.opts.Dir, 0o755); err != nil {
		return "", err
	}
	url := steamcmdURLMac
	if runtime.GOOS == "linux" {
		url = steamcmdURLLinux
	}
	tarball := filepath.Join(e.opts.Dir, ".steamcmd.tar.gz")
	if err := e.download(ctx, StepSteamCMD, url, tarball); err != nil {
		return "", err
	}
	defer os.Remove(tarball)

	stage, err := os.MkdirTemp(e.opts.Dir, ".steamcmd-stage-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if out, err := exec.CommandContext(ctx, "tar", "-xzf", tarball, "-C", stage).CombinedOutput(); err != nil {
		return "", fmt.Errorf("extracting SteamCMD: %v: %s", err, firstLine(out))
	}
	if !fileExecutable(filepath.Join(stage, "steamcmd.sh")) {
		return "", fmt.Errorf("SteamCMD archive did not contain steamcmd.sh")
	}
	os.RemoveAll(e.steamcmdDir())
	return "", os.Rename(stage, e.steamcmdDir())
}

func (e *Engine) downloadQuake1(ctx context.Context) (string, error) {
	if !e.opts.NeedsQuake1Data() {
		return "not selected", nil
	}
	if e.opts.EnginesOnly {
		return "engines only", nil
	}
	if err := verifyQuake1Data(e.quake1Dir()); err == nil {
		return "already downloaded", nil
	}
	if err := e.steamAppUpdate(ctx, StepQuake1, Quake1AppID, e.quake1Dir()); err != nil {
		return "", err
	}
	if err := verifyQuake1Data(e.quake1Dir()); err != nil {
		return "", fmt.Errorf("Quake data incomplete after download (does this account own Quake, AppID %s?): %w", Quake1AppID, err)
	}
	return "", nil
}

func (e *Engine) downloadQuake3(ctx context.Context) (string, error) {
	if !e.opts.Quake3 {
		return "not selected", nil
	}
	if e.opts.EnginesOnly {
		return "engines only", nil
	}
	if err := verifyQuake3Data(e.quake3Dir()); err == nil {
		return "already downloaded", nil
	}
	if err := e.steamAppUpdate(ctx, StepQuake3, Quake3AppID, e.quake3Dir()); err != nil {
		return "", err
	}
	if err := verifyQuake3Data(e.quake3Dir()); err != nil {
		return "", fmt.Errorf("Quake III data incomplete after download (does this account own Quake III Arena, AppID %s?): %w", Quake3AppID, err)
	}
	return "", nil
}

func (e *Engine) quake1Dir() string { return filepath.Join(e.opts.Dir, "quake1") }
func (e *Engine) quake3Dir() string { return filepath.Join(e.opts.Dir, "quake3") }

// steamUser returns the Steam account name, prompting lazily the first time
// a download actually needs one (so engine-repair reruns never ask).
func (e *Engine) steamUser(ctx context.Context, step StepID) (string, error) {
	if e.opts.SteamUser != "" {
		return e.opts.SteamUser, nil
	}
	e.log(step, "A Steam account that owns the game is required (credentials go directly to Valve's SteamCMD).")
	user, err := e.ask(ctx, step, "Steam username", false)
	if err != nil {
		return "", err
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return "", fmt.Errorf("a Steam username is required")
	}
	e.opts.SteamUser = user
	return user, nil
}

// steamAppUpdate runs SteamCMD under a pseudo-terminal, translating its
// output into progress events and proxying password / Steam Guard prompts
// through the UI. SteamCMD's exit code is unreliable, so success is judged
// by its own "fully installed" message plus on-disk verification afterwards.
func (e *Engine) steamAppUpdate(ctx context.Context, step StepID, appID, installDir string) error {
	user, err := e.steamUser(ctx, step)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx,
		filepath.Join(e.steamcmdDir(), "steamcmd.sh"),
		"+@sSteamCmdForcePlatformType", "windows",
		"+force_install_dir", installDir,
		"+login", user,
		"+app_update", appID, "validate",
		"+quit",
	)
	tty, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("starting SteamCMD: %w", err)
	}
	defer tty.Close()

	parser := newSteamParser()
	buf := make([]byte, 4096)
	for {
		n, rerr := tty.Read(buf)
		if n > 0 {
			for _, act := range parser.feed(string(buf[:n])) {
				switch act.kind {
				case actProgress:
					e.progress(step, act.verb, act.done, act.total)
				case actLog:
					e.log(step, act.line)
				case actPrompt:
					answer, aerr := e.ask(ctx, step, act.line, act.masked)
					if aerr != nil {
						cmd.Process.Kill()
						cmd.Wait()
						return aerr
					}
					if _, werr := io.WriteString(tty, answer+"\n"); werr != nil {
						cmd.Process.Kill()
						cmd.Wait()
						return fmt.Errorf("sending input to SteamCMD: %w", werr)
					}
				}
			}
		}
		if rerr != nil {
			break // EOF / EIO when SteamCMD exits; success judged below
		}
	}
	cmd.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if parser.failure != "" {
		return fmt.Errorf("SteamCMD: %s", parser.failure)
	}
	if !parser.sawSuccess(appID) {
		return fmt.Errorf("SteamCMD did not report app %s as installed (last output: %s)", appID, parser.lastLine)
	}
	return nil
}

func fileExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}
