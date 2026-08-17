# quakeup

One command to get **Quake** (the 2021 rerelease) and **Quake III Arena** running natively on your Mac — using your own Steam copies of the games.

Neither game has a native macOS build on Steam. quakeup bridges that gap:

1. Downloads [SteamCMD](https://developer.valvesoftware.com/wiki/SteamCMD) and logs into **your** Steam account (credentials go straight to Valve's client — quakeup never sees or stores them)
2. Fetches the games' Windows depots — the platform-independent data files are all that matter
3. Installs two excellent native engines: [vkQuake](https://github.com/Novum/vkQuake) (via [MacSourcePorts](https://macsourceports.com)) and [ioquake3](https://ioquake3.org) — both Developer ID signed and notarized, verified with `codesign`/`spctl` after install
4. Wires the data into the engines and drops `play-quake1.sh` / `play-quake3.sh` launchers

You get the full package: the remastered Quake campaign, both mission packs, *Dimension of the Machine*, the soundtrack, and Quake III Arena at point release 1.32.

## Install

```sh
brew install arthursoares/tap/quakeup
```

Or grab a binary from [releases](https://github.com/arthursoares/quakeup/releases), or build from source with `go build`.

## Usage

```sh
quakeup                      # interactive installer, installs to ~/Games/Quake
quakeup --dir ~/SomeDir      # custom install directory
quakeup --engines-only       # repair/reinstall just the engines
quakeup --games-only         # just the Steam downloads
quakeup --plain              # plain log output (no TUI), also used when piped
```

Then:

```sh
~/Games/Quake/play-quake1.sh             # Quake (remastered)
~/Games/Quake/play-quake1.sh -hipnotic   # Scourge of Armagon
~/Games/Quake/play-quake1.sh -rogue      # Dissolution of Eternity
~/Games/Quake/play-quake1.sh -game mg1   # Dimension of the Machine
~/Games/Quake/play-quake3.sh             # Quake III Arena
```

## Requirements

- macOS (Apple Silicon or Intel). On Apple Silicon, SteamCMD needs Rosetta 2 — quakeup offers to install it. Both game engines are native universal binaries.
- A Steam account that **owns** [Quake](https://store.steampowered.com/app/2310/) and/or [Quake III Arena](https://store.steampowered.com/app/2200/). quakeup automates the download you're already entitled to; it is not a way to obtain the games without buying them.

quakeup is safe to re-run: completed steps are detected and skipped, and interrupted downloads are retried.

## How it works

The interesting part is SteamCMD handling: quakeup runs it under a pseudo-terminal, parses its `Update state (...) downloading, progress: ...` output into a real progress bar, and proxies the password / Steam Guard prompts through the UI. Success is judged by SteamCMD's own "fully installed" message *plus* verification of the expected files on disk (all nine `pak0–8.pk3` for Quake 3, the rerelease tree for Quake 1) — SteamCMD's exit code is famously unreliable.

## Development

```sh
go test ./...
go build
```

Releases are cut with [goreleaser](https://goreleaser.com), which builds a universal binary and updates the [Homebrew tap](https://github.com/arthursoares/homebrew-tap).

## License

MIT. quakeup contains no id Software assets; game data is downloaded from Steam with your own license.
