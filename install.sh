#!/bin/sh
# Builds idios on this machine and installs it to /Applications.
#
# The app is unsigned, so it is built locally: an app built here never
# carries the quarantine attribute and Gatekeeper has nothing to block.
# Run from a checkout, or standalone (curl | sh), which clones first.
set -eu

REPO=https://github.com/hasanMshawrab/idios.git
APP=/Applications/idios.app

fail() {
    echo "install.sh: $1" >&2
    exit 1
}

[ "$(uname -s)" = "Darwin" ] || fail "idios's app is macOS-only"
command -v git >/dev/null || fail "git is required"
command -v go >/dev/null || \
    fail "Go is required (brew install go); the build pulls the pinned toolchain itself"
command -v xcodebuild >/dev/null || fail "Xcode is required for the app (App Store)"
xcodebuild -version >/dev/null 2>&1 || \
    fail "xcodebuild found no Xcode; run: sudo xcode-select -s /Applications/Xcode.app"

# A checkout is recognized by the module line; anywhere else, clone.
if [ -f go.mod ] && [ "$(head -1 go.mod)" = "module github.com/hasanMshawrab/idios" ]; then
    SRC=$PWD
else
    SRC=$(mktemp -d "${TMPDIR:-/tmp}/idios-install.XXXXXX")/idios
    echo "cloning $REPO"
    git clone --depth 1 "$REPO" "$SRC"
fi
cd "$SRC"

echo "building the daemon"
go build -o bin/idios ./cmd/idios

echo "building the app (Release)"
xcodebuild -project macos/idios.xcodeproj -scheme idios -configuration Release \
    -derivedDataPath macos/DerivedData -skipPackagePluginValidation -quiet build

BUILT=macos/DerivedData/Build/Products/Release/idios.app
[ -d "$BUILT" ] || fail "the build produced no app at $BUILT"

# The daemon joins the bundle as a resource, then both are signed ad hoc:
# adding a file after signing would break the seal the build put on it.
STAGE=$(mktemp -d "${TMPDIR:-/tmp}/idios-stage.XXXXXX")
ditto "$BUILT" "$STAGE/idios.app"
ditto bin/idios "$STAGE/idios.app/Contents/Resources/idios"
codesign --force -s - "$STAGE/idios.app/Contents/Resources/idios" 2>/dev/null
codesign --force -s - "$STAGE/idios.app" 2>/dev/null

[ -w /Applications ] || fail "/Applications is not writable; rerun with sudo"
rm -rf "$APP"
ditto "$STAGE/idios.app" "$APP"
rm -rf "$STAGE"

# The Dock and Launchpad cache the tile they saw first; a forced
# re-registration makes the new one show.
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister \
    -f "$APP" 2>/dev/null || true

# A PATH entry is a convenience, not a need: the app shows MCP configuration
# with the full path. The script never escalates itself; it prints the opt-in.
CLI=$APP/Contents/Resources/idios
if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    ln -sf "$CLI" /usr/local/bin/idios
    echo "linked /usr/local/bin/idios -> $CLI"
else
    echo "the CLI (for 'idios mcp') is at: $CLI"
    echo "to put it on PATH: sudo ln -sf \"$CLI\" /usr/local/bin/idios"
fi

echo
echo "installed $APP"
echo "data will live in ~/Library/Application Support/idios"
echo "to uninstall: rm -rf $APP ~/Library/Application\\ Support/idios"
