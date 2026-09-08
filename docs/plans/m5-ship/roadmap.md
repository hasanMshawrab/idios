# m5 - Ship

Status: complete 2026-08-31. All four steps shipped; the user reviewed
every commit and verified the installed app end to end, which surfaced
and fixed one bug (the spawned daemon's PATH).

Goal: the repository gets a remote and a README a stranger can read, and
the application gets an install story a stranger can run: clone plus
`./install.sh` puts an `idios.app` in `/Applications` that carries its own
daemon, asks for a kubeconfig on first launch, and keeps everything under
`~/Library/Application Support/idios` (already the daemon's default
`data_dir`). The app stays unsigned, so the whole story is built on
building locally.

## Decisions (made, do not relitigate)

1. The app owns the daemon. The Go binary is bundled inside `idios.app`
   at `Contents/Resources/idios`; on launch, when nothing answers on the
   connection address, the app spawns `idios run` (no flags: the defaults
   already point at `~/Library/Application Support/idios`) and terminates
   it with SIGTERM on quit (`main.go` already stops cleanly on SIGTERM).
   A daemon that is already answering is used as found, so the dev
   workflow (a manual daemon against `.storage`) is untouched; a
   `-daemon` launch argument or a non-default address disables spawning
   entirely, and so does a `-screenshot` run. A LaunchAgent is a
   stable-release question, not this milestone.
2. The kubeconfig choice lives in the daemon's own `idios.toml` in the
   default data dir, written by the app. The daemon needs zero new code
   (it already reads `<data-dir>/idios.toml`) and a hand-started daemon
   sees the same truth. The app rewrites only the `kubeconfig` line and
   preserves every other line verbatim.
3. Unsigned means built locally. `install.sh` builds on the user's
   machine (Xcode and Go are prerequisites), so the app never carries a
   quarantine attribute. It signs the embedded daemon and then the app
   ad hoc (`codesign -s -`). Run inside a checkout it builds in place;
   run standalone (the `curl | sh` of the future public repo) it clones
   to a temp dir first.
4. First run is two steps: kubeconfig (mandatory, no cancel, prefilled
   with `~/.kube/config` when it exists, file picker shows hidden files)
   then the existing add-cluster flow (skippable). Setup appears only
   when the app is about to spawn the daemon and `idios.toml` has no
   kubeconfig; connecting to an external daemon never triggers it.
5. README screenshots come from the smoke namespace in Light appearance
   via `hack/macos/screenshot.sh`, committed under `docs/screenshots/`.
   The mock was the first choice but serves the one-row contract
   fixtures, too thin for a hero image; smoke data is reproducible
   (`make smoke`, `kubectl -n idios-smoke apply -f hack/smoke/`) and
   contains nothing private. A daemon on another port reads
   `api_listen` from `<data-dir>/idios.toml`.
6. Milestone process: nothing is committed or pushed without the user's
   review of the diff; the remote (`github.com/hasanMshawrab/idios`,
   public) is created and pushed once, at the end.

## Steps

### A. README and screenshots

Take the light-mode screenshots against a daemon on `.storage/smoke`
with the smoke namespace failing (decision 5): `incidents` (hero),
`incident/<id>` (detail with timeline), `pod/<uid>` (evidence tabs),
`workloads`, `menubar`; ids come from `curl <addr>/v1/incidents`.
Confirm the system appearance is Light first
(`defaults read -g AppleInterfaceStyle` errors when it is);
pause and ask the user to switch when it is not. Then `README.md`: name
and one-line pitch, hero image, what idios records and why (a paragraph,
with `docs/design/` as the deep reference), the remaining screenshots
with a sentence each, Install (prerequisites: macOS 15, Xcode, Go; clone
and `./install.sh`; what first run asks), a note that `idios mcp` gives
an AI agent read-only access with the config snippet from m4, Development
(make targets, smoke cluster, screenshot tool), a status line saying
pre-release: unsigned, schema recreated on change.

### B. App-owned daemon and first-run setup (Swift)

The spec statement this implements lands in `docs/design/presentation.md`
as a short Distribution section (decisions 1, 2 and 4 above) in the same
change.

- `macos/idios/App/DaemonConfigFile.swift`: reads and writes the
  `kubeconfig = "<path>"` line of
  `~/Library/Application Support/idios/idios.toml`, creating file and
  directory when absent, preserving unknown lines. Pure functions over
  `String` for parse and rewrite so the tests need no filesystem.
  Tests: absent file, empty file, line present, line rewritten with
  other lines kept byte-for-byte, a path containing spaces.
- `macos/idios/App/DaemonManager.swift`: probes `/v1/status` on the
  connection address; decides `connect` (something answered),
  `setup` (nothing answered, spawn allowed, no kubeconfig in the toml)
  or `spawn`. Spawns `Bundle.main.resourceURL/idios` with `["run"]`
  via `Process`, SIGTERMs it on quit, and surfaces a spawn failure as
  the existing not-connected state. Never spawns when the address is
  not the default, when `-daemon` or `-screenshot` was passed, or when
  the bundled binary is missing (a `make app` dev build). The decision
  is a pure function (probe result, override flags, binary present,
  kubeconfig present) -> action; tests cover the whole truth table.
- Setup UI (`macos/idios/Views/Setup/`): a sheet on the main window the
  user cannot dismiss until step 1 completes. Step 1: kubeconfig path
  with prefill and `NSOpenPanel` (`showsHiddenFiles = true`), validates
  the file exists and is readable, writes the toml, asks DaemonManager
  to spawn, waits for `/v1/status`. Step 2: the existing add-cluster
  content, with a Skip button.
- Settings: a Kubeconfig row (current path, Change button; changing
  rewrites the toml and restarts a spawned daemon; disabled with a note
  when the daemon is external) and a Start at login toggle
  (`SMAppService.mainApp`).

### C. install.sh

`install.sh` at the repo root, `sh`-compatible. When the working
directory is not an idios checkout (no `go.mod` naming the module),
clone `https://github.com/hasanMshawrab/idios` into a temp dir and
continue there. Check for `xcodebuild` and `go`, with one clear message
each naming what to install. Build: `go build -o bin/idios ./cmd/idios`,
then `xcodebuild ... -configuration Release -skipPackagePluginValidation`
into its own DerivedData path. Assemble: copy the app to a staging dir,
`ditto` `bin/idios` to `Contents/Resources/idios`, `codesign -s -` the
daemon then `codesign --force -s -` the app. Install: replace
`/Applications/idios.app`, `lsregister -f` it (the Dock caches tiles),
offer a `/usr/local/bin/idios` symlink to the embedded binary when the
directory is writable (the `idios mcp` config expects `idios` on PATH),
otherwise print the full path. End by printing where data lives and the
one-line uninstall (`rm -rf` the app and the data dir). The README's
Install section and this script must agree; A writes the words, C makes
them true.

### D. Remote

`gh repo create hasanMshawrab/idios --public --source=. --push`, after
the user has reviewed and everything above is committed. Nothing else.

## Order and status

A, then B, then C (C embeds what B expects and the README promises),
then D. Checkpoint before every commit per `CLAUDE.md`, plus
`make app-test && make app` for B and C; every commit waits for the
user's review.

- Step A: done
- Step B: done
- Step C: done
- Step D: done
