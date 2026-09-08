#!/bin/sh
# Usage: hack/macos/screenshot.sh <route> <png> [--allow-disconnected] [--explain]
# Builds the application, runs it with -screenshot until it prints the number of
# its window, and photographs that window with screencapture. The window server
# owns the pixels of the sidebar and the toolbar, so the application cannot
# render its own review image.
# A screenshot of the not-connected screen is a valid run only when it is
# asked for, hence --allow-disconnected.
# --explain opens the route's help marks before the capture; the run fails when
# the application reported that a screen's regions and its explanations
# disagree.
# IDIOS_DAEMON, when set, is the address the application talks to.
set -eu

if [ $# -lt 2 ]; then
    echo "usage: $0 <route> <png> [--allow-disconnected] [--explain]" >&2
    exit 2
fi
ROUTE=$1
PNG=$2
shift 2

ALLOW_DISCONNECTED=""
EXPLAIN=""
while [ $# -gt 0 ]; do
    case $1 in
        --allow-disconnected) ALLOW_DISCONNECTED=yes ;;
        --explain) EXPLAIN=yes ;;
        *)
            echo "$0: unknown flag $1" >&2
            exit 2
            ;;
    esac
    shift
done

make app

DAEMON=${IDIOS_DAEMON:-127.0.0.1:7770}
if [ -z "$ALLOW_DISCONNECTED" ]; then
    if ! curl -sf "$DAEMON/v1/status" >/dev/null; then
        echo "$0: nothing answers on $DAEMON; start the daemon or pass --allow-disconnected" >&2
        exit 1
    fi
fi

OUT=$(mktemp "${TMPDIR:-/tmp}/idios-screenshot.XXXXXX")
trap 'rm -f "$OUT"' EXIT

APP=macos/DerivedData/Build/Products/Debug/idios.app/Contents/MacOS/idios
set -- -screenshot "$PNG" -route "$ROUTE"
if [ -n "${IDIOS_DAEMON:-}" ]; then
    set -- "$@" -daemon "$IDIOS_DAEMON"
fi
if [ -n "$EXPLAIN" ]; then
    set -- "$@" -explain
fi
"$APP" "$@" >"$OUT" 2>&1 &
APP_PID=$!

# A cold launch after a build, a store that answers no sooner than its cap and
# the wait for the help marks add up, so the poll is thirty seconds.
WINDOW=""
I=0
while [ $I -lt 300 ]; do
    WINDOW=$(sed -n 's/^idios-window \([0-9][0-9]*\)$/\1/p' "$OUT" | head -1)
    if [ -n "$WINDOW" ]; then
        break
    fi
    sleep 0.1
    I=$((I + 1))
done

if [ -z "$WINDOW" ]; then
    kill "$APP_PID" 2>/dev/null || true
    wait "$APP_PID" 2>/dev/null || true
    echo "$0: the application never reported its window number" >&2
    cat "$OUT" >&2
    exit 1
fi

screencapture -x -o -l "$WINDOW" "$PNG"
kill "$APP_PID" 2>/dev/null || true
wait "$APP_PID" 2>/dev/null || true

# A coverage line is a hole in the tables or a region nothing explains, which
# is a failed run even though the image was taken.
if grep '^idios-explain ' "$OUT" >&2; then
    exit 1
fi
