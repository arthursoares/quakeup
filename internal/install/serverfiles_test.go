package install

import (
	"strings"
	"testing"
)

func TestChecksumFor(t *testing.T) {
	manifest := "abc123  ./ezQuake-linux-x86_64.zip\nDEF456  ./ezQuake-macOS-universal.zip\n789fed *starred-file.zip\n"
	got, err := checksumFor(manifest, "ezQuake-macOS-universal.zip")
	if err != nil || got != "def456" {
		t.Fatalf("got %q, %v; want def456 (lowercased, ./ stripped)", got, err)
	}
	if got, err := checksumFor(manifest, "starred-file.zip"); err != nil || got != "789fed" {
		t.Fatalf("BSD-style '*' prefix: got %q, %v", got, err)
	}
	if _, err := checksumFor(manifest, "missing.zip"); err == nil {
		t.Fatal("missing file should error")
	}
}

func TestComposeFileBothServices(t *testing.T) {
	c := composeFile(true, true)
	for _, want := range []string{
		"florianpiesche/ioquake3-server",
		"niclaslindstedt/nquakesv",
		"../quake3/baseq3:/opt/quake3/baseq3:ro",
		"27960:27960/udp",
		"27500:27500/udp",
		"platform: linux/amd64",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("compose missing %q", want)
		}
	}
}

func TestComposeFileSelective(t *testing.T) {
	q3only := composeFile(false, true)
	if strings.Contains(q3only, "nquakesv") {
		t.Error("q3-only compose contains the QuakeWorld service")
	}
	qwOnly := composeFile(true, false)
	if strings.Contains(qwOnly, "ioquake3") {
		t.Error("qw-only compose contains the Quake 3 service")
	}
	if !strings.Contains(qwOnly, "nquakesv") {
		t.Error("qw-only compose missing the QuakeWorld service")
	}
}

func TestNeedsQuake1Data(t *testing.T) {
	if (Options{Quake3: true}).NeedsQuake1Data() {
		t.Error("quake3-only should not need Quake 1 data")
	}
	if !(Options{EzQuake: true}).NeedsQuake1Data() {
		t.Error("ezQuake needs the Quake 1 depot for its paks")
	}
	if !(Options{Quake1: true}).NeedsQuake1Data() {
		t.Error("quake1 selection needs Quake 1 data")
	}
}
