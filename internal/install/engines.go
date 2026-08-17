package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func (e *Engine) installVkQuake(ctx context.Context) (string, error) {
	return e.installApp(ctx, StepVkQuake, appSpec{
		name:       "vkQuake.app",
		executable: "vkquake",
		url:        vkQuakeURL,
		kind:       "dmg",
	})
}

func (e *Engine) installIoquake3(ctx context.Context) (string, error) {
	return e.installApp(ctx, StepIoquake3, appSpec{
		name:       "ioquake3.app",
		executable: "ioquake3",
		url:        ioquake3URL,
		kind:       "zip",
	})
}

type appSpec struct {
	name       string // bundle name, e.g. "vkQuake.app"
	executable string // binary under Contents/MacOS/
	url        string
	kind       string // "dmg" or "zip"
}

func (a appSpec) binary(appsDir string) string {
	return filepath.Join(appsDir, a.name, "Contents", "MacOS", a.executable)
}

func (e *Engine) installApp(ctx context.Context, step StepID, spec appSpec) (string, error) {
	if e.opts.GamesOnly {
		return "games only", nil
	}
	dest := filepath.Join(e.opts.AppsDir, spec.name)
	// An existing bundle counts as installed only if its executable exists —
	// a half-copied bundle from an interrupted run must be replaced.
	if fileExecutable(spec.binary(e.opts.AppsDir)) {
		return "already installed", nil
	}
	if err := os.MkdirAll(e.opts.AppsDir, 0o755); err != nil {
		return "", err
	}

	archive := filepath.Join(e.opts.AppsDir, ".quakeup-"+spec.name+"."+spec.kind)
	if err := e.download(ctx, step, spec.url, archive); err != nil {
		return "", err
	}
	defer os.Remove(archive)

	// Stage next to the destination so the final os.Rename is atomic.
	stage, err := os.MkdirTemp(e.opts.AppsDir, ".quakeup-stage-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	e.log(step, "extracting…")
	var extracted string
	switch spec.kind {
	case "dmg":
		extracted, err = e.extractDMG(ctx, archive, stage, spec.name)
	case "zip":
		extracted, err = e.extractZip(ctx, archive, stage, spec.name)
	default:
		err = fmt.Errorf("unknown archive kind %q", spec.kind)
	}
	if err != nil {
		return "", err
	}

	// Both distributions are Developer ID signed and notarized; verify
	// before the bundle reaches its final name. This is the integrity check
	// for artifacts whose URLs upstream overwrites on update.
	e.log(step, "verifying code signature…")
	if out, err := exec.CommandContext(ctx, "codesign", "--verify", "--deep", "--strict", extracted).CombinedOutput(); err != nil {
		return "", fmt.Errorf("code signature verification failed for %s: %s", spec.name, firstLine(out))
	}
	if out, err := exec.CommandContext(ctx, "spctl", "--assess", "--type", "execute", extracted).CombinedOutput(); err != nil {
		return "", fmt.Errorf("Gatekeeper assessment failed for %s: %s", spec.name, firstLine(out))
	}

	os.RemoveAll(dest)
	if err := os.Rename(extracted, dest); err != nil {
		return "", err
	}
	if !fileExecutable(spec.binary(e.opts.AppsDir)) {
		return "", fmt.Errorf("%s installed but %s is missing or not executable", spec.name, spec.executable)
	}
	return "", nil
}

// extractDMG mounts the image read-only, copies the bundle out with ditto
// (which preserves signatures, extended attributes, and resource forks),
// and always detaches — including on error or cancellation.
func (e *Engine) extractDMG(ctx context.Context, dmg, stage, bundleName string) (string, error) {
	mount := filepath.Join(stage, "mnt")
	if out, err := exec.CommandContext(ctx, "hdiutil", "attach", dmg,
		"-nobrowse", "-readonly", "-quiet", "-mountpoint", mount).CombinedOutput(); err != nil {
		return "", fmt.Errorf("mounting %s: %v: %s", filepath.Base(dmg), err, firstLine(out))
	}
	defer exec.Command("hdiutil", "detach", mount, "-quiet", "-force").Run()

	src := filepath.Join(mount, bundleName)
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("%s not found inside disk image", bundleName)
	}
	dst := filepath.Join(stage, bundleName)
	if out, err := exec.CommandContext(ctx, "ditto", src, dst).CombinedOutput(); err != nil {
		return "", fmt.Errorf("copying %s: %v: %s", bundleName, err, firstLine(out))
	}
	return dst, nil
}

// extractZip unpacks with ditto -xk, which round-trips notarization tickets
// and permissions that archive/zip would drop, then locates the bundle.
func (e *Engine) extractZip(ctx context.Context, zip, stage, bundleName string) (string, error) {
	out := filepath.Join(stage, "unzipped")
	if msg, err := exec.CommandContext(ctx, "ditto", "-x", "-k", zip, out).CombinedOutput(); err != nil {
		return "", fmt.Errorf("extracting %s: %v: %s", filepath.Base(zip), err, firstLine(msg))
	}
	var found string
	filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == bundleName {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("%s not found inside archive", bundleName)
	}
	return found, nil
}
