package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func touch(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyQuake1Data(t *testing.T) {
	dir := t.TempDir()
	if verifyQuake1Data(dir) == nil {
		t.Fatal("empty dir passed verification")
	}
	// Classic tree alone must NOT pass: the launcher uses the rerelease.
	touch(t, filepath.Join(dir, "id1", "PAK0.PAK"), "x")
	if verifyQuake1Data(dir) == nil {
		t.Fatal("classic-only tree passed verification")
	}
	touch(t, filepath.Join(dir, "rerelease", "id1", "pak0.pak"), "x")
	touch(t, filepath.Join(dir, "rerelease", "QuakeEX.kpf"), "x")
	if err := verifyQuake1Data(dir); err != nil {
		t.Fatalf("complete tree failed verification: %v", err)
	}
}

func TestVerifyQuake3DataRequiresAllPaks(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "baseq3", "pak0.pk3"), "x")
	if verifyQuake3Data(dir) == nil {
		t.Fatal("pak0 alone passed verification")
	}
	for i := 1; i <= 8; i++ {
		touch(t, filepath.Join(dir, "baseq3", "pak"+string(rune('0'+i))+".pk3"), "x")
	}
	if err := verifyQuake3Data(dir); err != nil {
		t.Fatalf("complete pak set failed verification: %v", err)
	}
	// Zero-byte files (truncated copies) must fail.
	touch(t, filepath.Join(dir, "baseq3", "pak4.pk3"), "")
	if verifyQuake3Data(dir) == nil {
		t.Fatal("zero-byte pak passed verification")
	}
}

func TestShellQuote(t *testing.T) {
	cases := []string{
		"/plain/path",
		"/path with spaces/Quake",
		`/tricky/$HOME/"quotes"/it's/back\slash`,
	}
	for _, c := range cases {
		out, err := exec.Command("/bin/sh", "-c", "printf %s "+shellQuote(c)).Output()
		if err != nil {
			t.Fatalf("shell failed on %q: %v", c, err)
		}
		if string(out) != c {
			t.Errorf("round-trip of %q gave %q", c, string(out))
		}
	}
}

func TestPlaceholderQ3KeyAlphabet(t *testing.T) {
	// Quake 3's local CD-key validation accepts only these characters.
	const valid = "237ABCDGHJLPRSTW"
	if len(placeholderQ3Key) != 16 {
		t.Fatalf("key must be 16 chars, got %d", len(placeholderQ3Key))
	}
	for _, ch := range placeholderQ3Key {
		if !strings.ContainsRune(valid, ch) {
			t.Errorf("character %q is outside Quake 3's valid CD-key alphabet", ch)
		}
	}
}

func TestCopyFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.pk3")
	dst := filepath.Join(dir, "dst.pk3")
	touch(t, src, "fresh data")
	touch(t, dst, "stale")
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "fresh data" {
		t.Errorf("dst = %q, want fresh copy", got)
	}
}
