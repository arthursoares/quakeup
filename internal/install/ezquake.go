package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func (e *Engine) quakeworldDir() string { return filepath.Join(e.opts.Dir, "quakeworld") }

// installEzQuake sets up the QuakeWorld client in <dir>/quakeworld: the
// official universal .app (checksum-verified against the release manifest)
// next to the classic id1 paks, which is the layout ezQuake expects.
func (e *Engine) installEzQuake(ctx context.Context) (string, error) {
	switch {
	case !e.opts.EzQuake:
		return "not selected", nil
	case e.opts.GamesOnly:
		return "games only", nil
	case runtime.GOOS != "darwin":
		return "macOS only — on Linux install ezquake from your package manager", nil
	}
	app := filepath.Join(e.quakeworldDir(), "ezQuake.app")
	if fileExecutable(filepath.Join(app, "Contents", "MacOS", "ezQuake")) {
		return "already installed", nil
	}
	if err := os.MkdirAll(e.quakeworldDir(), 0o755); err != nil {
		return "", err
	}

	stage, err := os.MkdirTemp(e.quakeworldDir(), ".quakeup-stage-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	zip := filepath.Join(stage, ezQuakeZip)
	sums := filepath.Join(stage, "checksums.txt")
	if err := e.download(ctx, StepEzQuake, ezQuakeZipURL, zip); err != nil {
		return "", err
	}
	if err := e.download(ctx, StepEzQuake, ezQuakeSumURL, sums); err != nil {
		return "", err
	}

	e.log(StepEzQuake, "verifying checksum…")
	manifest, err := os.ReadFile(sums)
	if err != nil {
		return "", err
	}
	want, err := checksumFor(string(manifest), ezQuakeZip)
	if err != nil {
		return "", err
	}
	got, err := fileSHA256(zip)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("checksum mismatch for %s: got %s, want %s", ezQuakeZip, got, want)
	}

	if out, err := exec.CommandContext(ctx, "ditto", "-x", "-k", zip, filepath.Join(stage, "unzipped")).CombinedOutput(); err != nil {
		return "", fmt.Errorf("extracting %s: %v: %s", ezQuakeZip, err, firstLine(out))
	}
	var found string
	filepath.WalkDir(filepath.Join(stage, "unzipped"), func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == "ezQuake.app" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("ezQuake.app not found inside archive")
	}
	os.RemoveAll(app)
	if err := os.Rename(found, app); err != nil {
		return "", err
	}
	if !fileExecutable(filepath.Join(app, "Contents", "MacOS", "ezQuake")) {
		return "", fmt.Errorf("ezQuake.app installed but its binary is missing or not executable")
	}
	return "", nil
}

// checksumFor extracts the SHA-256 for filename from a standard
// "<hash>  <name>" checksum manifest. Names may carry a "*" (BSD binary
// mode) or "./" prefix — ezQuake's manifest uses the latter.
func checksumFor(manifest, filename string) (string, error) {
	for _, line := range strings.Split(manifest, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(fields[1], "*"), "./")
		if name == filename {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in manifest", filename)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
