package install

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Community add-ons for Quake 3, all pinned to immutable URLs with hard
// SHA-256s (verified at install time):
//
//   - CPMA 1.53 and its map pack from the official playmorepromode.com CDN
//   - Cosmetic packs (Quake Live sounds, HD textures, HD weapons) from the
//     diegoulloao/ioquake3-mac-install repo, pinned to a commit SHA
//
// The cosmetic pk3s install under zzz- names: Quake 3 gives alphabetically
// later pk3s precedence, so this guarantees they override pak0–pak8.
type extraArtifact struct {
	label  string
	url    string
	sha256 string
	kind   string // "cpma" | "mappack" | "pk3"
	dest   string // pk3 only: final filename in baseq3
	marker string // path relative to the Quake3 home dir; present = installed
}

const extrasRepoRaw = "https://raw.githubusercontent.com/diegoulloao/ioquake3-mac-install/3a767ff0131742ec517fd5f13ddca16dee91927d/extras/"

var extraArtifacts = []extraArtifact{
	{
		label:  "CPMA 1.53",
		url:    "https://cdn.playmorepromode.com/files/cpma/cpma-1.53-nomaps.zip",
		sha256: "edfffa0c1a0375ba46a5b42257a168fb15086712245733526ab2d9ccdd821ca0",
		kind:   "cpma",
		marker: "cpma/z-cpma-pak153.pk3",
	},
	{
		label:  "CPMA map pack",
		url:    "https://cdn.playmorepromode.com/files/cpma-mappack-full.zip",
		sha256: "5db933fc92c41f2e0941ab65725586d4d0c30fe84727427bb6b265e4d941a226",
		kind:   "mappack",
		marker: "baseq3/map_cpm22.pk3",
	},
	{
		label:  "Quake Live sounds",
		url:    extrasRepoRaw + "quake3-live-sounds.pk3",
		sha256: "d2f383229037ff904f367a393ca85cfc8488dab1585d8cae98a773505c979f73",
		kind:   "pk3",
		dest:   "zzz-quake3-live-sounds.pk3",
		marker: "baseq3/zzz-quake3-live-sounds.pk3",
	},
	{
		label:  "HD textures",
		url:    extrasRepoRaw + "extra-pack-resolution.pk3",
		sha256: "ba2d2cdff96942f1144ffeaf5d103533409ec8fb14a56e08813048fe7d4523cc",
		kind:   "pk3",
		dest:   "zzz-extra-pack-resolution.pk3",
		marker: "baseq3/zzz-extra-pack-resolution.pk3",
	},
	{
		label:  "HD weapons",
		url:    extrasRepoRaw + "hd-weapons.pk3",
		sha256: "1784c853e2ef041a37b16bc2b915d3a37c86495a6d601df4bf5b419d56c02970",
		kind:   "pk3",
		dest:   "zzz-hd-weapons.pk3",
		marker: "baseq3/zzz-hd-weapons.pk3",
	},
}

// q3Home is the directory holding baseq3 and mod folders (the parent of
// Q3UserData, e.g. ~/Library/Application Support/Quake3 or ~/.q3a).
func (e *Engine) q3Home() string { return filepath.Dir(e.opts.Q3UserData) }

func (e *Engine) installExtras(ctx context.Context) (string, error) {
	if !e.opts.Extras {
		return "not selected", nil
	}
	if !e.opts.Quake3 && verifyQuake3Data(e.quake3Dir()) != nil {
		return "", fmt.Errorf("extras need Quake III Arena — select it too, or point --dir at an install that has it")
	}
	installed := 0
	for _, a := range extraArtifacts {
		if _, err := os.Stat(filepath.Join(e.q3Home(), a.marker)); err == nil {
			continue // already installed
		}
		if err := e.installExtra(ctx, a); err != nil {
			return "", fmt.Errorf("%s: %w", a.label, err)
		}
		e.log(StepExtras, a.label+" installed")
		installed++
	}
	if installed == 0 {
		return "already installed", nil
	}
	return "", nil
}

func (e *Engine) installExtra(ctx context.Context, a extraArtifact) error {
	baseq3 := e.opts.Q3UserData
	if err := os.MkdirAll(baseq3, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(baseq3, ".quakeup-extra-download")
	if err := e.download(ctx, StepExtras, a.url, tmp); err != nil {
		return err
	}
	defer os.Remove(tmp)
	got, err := fileSHA256(tmp)
	if err != nil {
		return err
	}
	if got != a.sha256 {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, a.sha256)
	}

	switch a.kind {
	case "pk3":
		return os.Rename(tmp, filepath.Join(baseq3, a.dest))
	case "cpma":
		// The archive contains a top-level cpma/ folder; extract it next
		// to baseq3.
		return unzipInto(tmp, e.q3Home(), func(name string) bool {
			return strings.HasPrefix(name, "cpma/")
		})
	case "mappack":
		// pk3s at the archive root go into baseq3.
		return unzipInto(tmp, baseq3, func(name string) bool {
			return !strings.Contains(name, "/") && strings.HasSuffix(name, ".pk3")
		})
	}
	return fmt.Errorf("unknown artifact kind %q", a.kind)
}

// unzipInto extracts entries matching want into dest, refusing paths that
// escape it (zip-slip).
func unzipInto(zipPath, dest string, want func(string) bool) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if !want(name) || strings.HasSuffix(name, "/") {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		rel, err := filepath.Rel(dest, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("archive entry escapes destination: %s", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			src.Close()
			return err
		}
		_, cerr := io.Copy(out, src)
		src.Close()
		if err := out.Close(); err != nil {
			return err
		}
		if cerr != nil {
			return cerr
		}
	}
	return nil
}
