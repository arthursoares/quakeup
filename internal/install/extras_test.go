package install

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestExtraArtifactPins(t *testing.T) {
	hash := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, a := range extraArtifacts {
		if !hash.MatchString(a.sha256) {
			t.Errorf("%s: malformed sha256 pin %q", a.label, a.sha256)
		}
		if !strings.HasPrefix(a.url, "https://") {
			t.Errorf("%s: non-https URL %q", a.label, a.url)
		}
		if strings.Contains(a.url, "githubusercontent") && !strings.Contains(a.url, "/3a767ff") {
			t.Errorf("%s: GitHub URL not pinned to a commit SHA: %q", a.label, a.url)
		}
		if a.marker == "" {
			t.Errorf("%s: no install marker", a.label)
		}
		if a.kind == "pk3" && !strings.HasPrefix(a.dest, "zzz-") {
			t.Errorf("%s: pk3 dest %q must be zzz-prefixed to override pak0-8", a.label, a.dest)
		}
	}
}

func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	w.Close()
	path := filepath.Join(t.TempDir(), "test.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUnzipIntoFiltersAndExtracts(t *testing.T) {
	z := makeZip(t, map[string]string{
		"cpma/z-cpma-pak153.pk3": "mod data",
		"cpma/docs/readme.txt":   "docs",
		"other/skip.txt":         "skip me",
		"rootfile.pk3":           "root",
	})
	dest := t.TempDir()
	err := unzipInto(z, dest, func(name string) bool { return strings.HasPrefix(name, "cpma/") })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "cpma", "z-cpma-pak153.pk3")); err != nil {
		t.Error("wanted entry not extracted")
	}
	if _, err := os.Stat(filepath.Join(dest, "other")); !os.IsNotExist(err) {
		t.Error("filtered entry was extracted")
	}
}

func TestUnzipIntoRejectsZipSlip(t *testing.T) {
	z := makeZip(t, map[string]string{"../evil.txt": "escape"})
	dest := t.TempDir()
	err := unzipInto(z, dest, func(string) bool { return true })
	if err == nil {
		t.Fatal("zip-slip entry was not rejected")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dest), "evil.txt")); !os.IsNotExist(statErr) {
		t.Fatal("zip-slip file escaped the destination")
	}
}
