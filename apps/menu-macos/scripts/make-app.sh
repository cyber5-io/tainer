#!/bin/bash
# Assemble build/Tainer Menu.app from the SwiftPM release build.
#   make-app.sh           build and assemble the bundle
#   make-app.sh --check   assemble, then assert the bundle structure (launches nothing)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/build/Tainer Menu.app"
PLIST="$APP/Contents/Info.plist"

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
cp -R "$BIN_DIR/TainerMenu_TainerMenu.bundle" "$APP/Contents/Resources/"

if [ "${1:-}" = "--check" ]; then
    check
else
    echo "Assembled $APP"
fi
