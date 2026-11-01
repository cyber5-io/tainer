#!/bin/bash
# Assemble build/Tainer Menu.app from the SwiftPM release build.
#   make-app.sh           build and assemble the bundle
#   make-app.sh --check   assemble, then assert the bundle structure (launches nothing)
#   --app-version <v>     stamp <v> into the bundle (default: VERSION in the repo-root Makefile)
set -euo pipefail

CHECK=0
APP_VERSION=""
while [ $# -gt 0 ]; do
    case "$1" in
        --check) CHECK=1 ;;
        --app-version) APP_VERSION="${2:?--app-version needs a value}"; shift ;;
        *) echo "unknown argument: $1" >&2; exit 2 ;;
    esac
    shift
done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/build/Tainer Menu.app"
PLIST="$APP/Contents/Info.plist"
MAKEFILE="$ROOT/../../Makefile"

if [ -z "$APP_VERSION" ]; then
    APP_VERSION="$(sed -nE 's/^VERSION[[:space:]]*:?=[[:space:]]*([0-9]+\.[0-9]+\.[0-9]+[^[:space:]]*)[[:space:]]*$/\1/p' "$MAKEFILE" | head -n1)"
fi
if ! [[ "$APP_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]]; then
    echo "FAIL: cannot determine tainer version (got '$APP_VERSION'); pass --app-version or fix the VERSION line in $MAKEFILE" >&2
    exit 1
fi

check() {
    local fail=0
    [ -x "$APP/Contents/MacOS/TainerMenu" ] || { echo "FAIL: executable missing"; fail=1; }
    # Must match AppBundle.resources: Bundle.main.resourceURL + resource bundle name.
    local rb="$APP/Contents/Resources/TainerMenu_TainerMenu.bundle"
    [ -d "$rb" ] || { echo "FAIL: resource bundle missing at $rb"; fail=1; }
    [ -f "$rb/bar-mark@2x.png" ] || [ -f "$rb/bar-mark.png" ] || { echo "FAIL: bar-mark not inside $rb"; fail=1; }
    [ "$(/usr/libexec/PlistBuddy -c 'Print :LSUIElement' "$PLIST")" = "true" ] || { echo "FAIL: LSUIElement is not true"; fail=1; }
    [ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$PLIST")" = "io.cyber5.tainer.menu" ] || { echo "FAIL: CFBundleIdentifier"; fail=1; }
    [ "$(/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' "$PLIST")" = "14.0" ] || { echo "FAIL: LSMinimumSystemVersion"; fail=1; }
    local stamped
    stamped="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$PLIST")"
    [ "$stamped" = "$APP_VERSION" ] && [ "$stamped" != "0.1.0" ] || { echo "FAIL: stamped version '$stamped' != $APP_VERSION (or is the 0.1.0 placeholder)"; fail=1; }
    [ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$PLIST")" = "$APP_VERSION" ] || { echo "FAIL: CFBundleVersion not stamped"; fail=1; }
    plutil -lint "$PLIST" >/dev/null || { echo "FAIL: Info.plist invalid"; fail=1; }
    if [ "$fail" -ne 0 ]; then exit 1; fi
    echo "OK: $APP"
    echo "  LSUIElement=true CFBundleIdentifier=io.cyber5.tainer.menu LSMinimumSystemVersion=14.0"
}

cd "$ROOT"
swift build -c release
BIN_DIR="$(swift build -c release --show-bin-path)"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN_DIR/TainerMenu" "$APP/Contents/MacOS/TainerMenu"
cp "$ROOT/Resources/Info.plist" "$PLIST"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $APP_VERSION" -c "Set :CFBundleVersion $APP_VERSION" "$PLIST"
cp -R "$BIN_DIR/TainerMenu_TainerMenu.bundle" "$APP/Contents/Resources/"

if [ "$CHECK" -eq 1 ]; then
    check
else
    echo "Assembled $APP"
fi
