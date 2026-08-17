// Package install implements the quakeup installation engine.
//
// The engine runs as a pipeline of steps and reports everything it does as
// Events on a channel. It never talks to a terminal directly: interactive
// input (Steam password, Steam Guard codes, yes/no questions) is requested
// via an Event carrying a reply channel, so the same engine drives both the
// Bubble Tea UI and the --plain fallback.
package install

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
)

// Application IDs and download sources. vkQuake is pinned to an immutable
// versioned artifact; SteamCMD and ioquake3 live at unversioned URLs that
// upstream overwrites on update, so their integrity is checked via
// codesign/spctl after install instead of a pinned hash.
const (
	Quake1AppID = "2310"
	Quake3AppID = "2200"

	steamcmdURLMac   = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd_osx.tar.gz"
	steamcmdURLLinux = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz"
	vkQuakeURL  = "https://github.com/MacSourcePorts/MSPBuildSystem/releases/download/vkQuake_1.35.0/vkQuake-1.35.0.dmg"
	ioquake3URL = "https://files.ioquake3.org/ioquake3_notarized.zip"

	// ezQuake is pinned to a versioned release and verified against the
	// release's own checksum manifest (its .app is ad-hoc signed, so
	// notarization checks don't apply).
	ezQuakeZipURL = "https://github.com/QW-Group/ezquake-source/releases/download/3.6.9/ezQuake-macOS-universal.zip"
	ezQuakeSumURL = "https://github.com/QW-Group/ezquake-source/releases/download/3.6.9/checksums.txt"
	ezQuakeZip    = "ezQuake-macOS-universal.zip"

	// Quake 3 CD keys use a restricted alphabet (2, 3, 7 and a subset of
	// consonants); this placeholder passes local validation in builds that
	// still carry the vestigial 1999 check. id's auth server is long gone,
	// so the key is never verified online.
	placeholderQ3Key = "2237ABCDGHJLPRST"
)

type StepID int

const (
	StepPreflight StepID = iota
	StepSteamCMD
	StepQuake1
	StepQuake3
	StepVkQuake
	StepIoquake3
	StepEzQuake
	StepServerFiles
	StepWiring
	stepCount
)

var stepTitles = [stepCount]string{
	"Checking prerequisites",
	"Installing SteamCMD",
	"Downloading Quake (2021 rerelease)",
	"Downloading Quake III Arena",
	"Installing vkQuake",
	"Installing ioquake3",
	"Installing ezQuake (QuakeWorld)",
	"Writing server files (docker-compose)",
	"Wiring game data",
}

func (s StepID) Title() string { return stepTitles[s] }

type EventKind int

const (
	EvStepStart EventKind = iota
	EvStepDone            // Msg optionally carries a short result note
	EvStepSkip            // step not needed; Msg says why
	EvProgress            // Pct/BytesDone/BytesTotal valid; Msg is a verb ("downloading")
	EvLog                 // informational line
	EvNeedInput           // engine blocks until a string is sent on Reply
	EvFatal               // installation failed; Msg is the error
	EvDone                // whole installation finished successfully
)

type Event struct {
	Kind       EventKind
	Step       StepID
	Msg        string
	Pct        float64 // 0..1, negative when unknown
	BytesDone  int64
	BytesTotal int64
	Masked     bool // NeedInput: hide typed characters
	Reply      chan<- string
}

// Options configures an installation run. Zero values mean defaults.
type Options struct {
	Dir         string // install root; default ~/Games/Quake
	AppsDir     string // where .app bundles go; default /Applications with ~/Applications fallback
	Q3UserData  string // ioquake3 data dir; default ~/Library/Application Support/Quake3/baseq3
	SteamUser   string // pre-supplied Steam account name; prompted lazily when empty
	EnginesOnly bool   // skip SteamCMD and game downloads
	GamesOnly   bool   // skip engine installation

	// Selection: which games and variations to set up. The caller (CLI
	// flags or the selection screen) decides; at least one must be set.
	Quake1      bool // Quake 2021 rerelease + vkQuake
	Quake3      bool // Quake III Arena + ioquake3
	EzQuake     bool // QuakeWorld deathmatch client (needs the Quake 1 Steam depot for paks)
	ServerFiles bool // generate docker-compose files for self-hosting servers
}

// NeedsQuake1Data reports whether the Quake 1 Steam depot is required
// (vkQuake plays from it and ezQuake borrows its classic id1 paks).
func (o Options) NeedsQuake1Data() bool { return o.Quake1 || o.EzQuake }

type Engine struct {
	opts   Options
	events chan Event
}

func New(opts Options) (*Engine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if opts.Dir == "" {
		opts.Dir = filepath.Join(home, "Games", "Quake")
	}
	if opts.AppsDir == "" {
		opts.AppsDir = "/Applications"
		if !dirWritable(opts.AppsDir) {
			opts.AppsDir = filepath.Join(home, "Applications")
		}
	}
	if opts.Q3UserData == "" {
		if runtime.GOOS == "linux" {
			opts.Q3UserData = filepath.Join(home, ".q3a", "baseq3")
		} else {
			opts.Q3UserData = filepath.Join(home, "Library", "Application Support", "Quake3", "baseq3")
		}
	}
	return &Engine{opts: opts, events: make(chan Event)}, nil
}

func (e *Engine) Events() <-chan Event { return e.events }
func (e *Engine) Options() Options    { return e.opts }

// Run executes the installation and closes the event channel when finished.
// It is intended to run in its own goroutine.
func (e *Engine) Run(ctx context.Context) {
	defer close(e.events)

	steps := []struct {
		id  StepID
		fn  func(context.Context) (skip string, err error)
	}{
		{StepPreflight, e.preflight},
		{StepSteamCMD, e.ensureSteamCMD},
		{StepQuake1, e.downloadQuake1},
		{StepQuake3, e.downloadQuake3},
		{StepVkQuake, e.installVkQuake},
		{StepIoquake3, e.installIoquake3},
		{StepEzQuake, e.installEzQuake},
		{StepServerFiles, e.writeServerFiles},
		{StepWiring, e.wire},
	}
	for _, s := range steps {
		if err := ctx.Err(); err != nil {
			e.emit(Event{Kind: EvFatal, Step: s.id, Msg: "cancelled"})
			return
		}
		e.emit(Event{Kind: EvStepStart, Step: s.id})
		skip, err := s.fn(ctx)
		switch {
		case err != nil:
			e.emit(Event{Kind: EvFatal, Step: s.id, Msg: err.Error()})
			return
		case skip != "":
			e.emit(Event{Kind: EvStepSkip, Step: s.id, Msg: skip})
		default:
			e.emit(Event{Kind: EvStepDone, Step: s.id})
		}
	}
	e.emit(Event{Kind: EvDone})
}

func (e *Engine) emit(ev Event) { e.events <- ev }

func (e *Engine) log(step StepID, msg string) {
	e.emit(Event{Kind: EvLog, Step: step, Msg: msg})
}

func (e *Engine) progress(step StepID, verb string, done, total int64) {
	pct := -1.0
	if total > 0 {
		pct = float64(done) / float64(total)
	}
	e.emit(Event{Kind: EvProgress, Step: step, Msg: verb, Pct: pct, BytesDone: done, BytesTotal: total})
}

// ask blocks until the UI supplies a line of input or the context ends.
func (e *Engine) ask(ctx context.Context, step StepID, prompt string, masked bool) (string, error) {
	reply := make(chan string, 1)
	e.emit(Event{Kind: EvNeedInput, Step: step, Msg: prompt, Masked: masked, Reply: reply})
	select {
	case s := <-reply:
		return s, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".quakeup-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
