#!/bin/bash
# release-files.sh <outdir> - assemble the folder to upload to get.tainer.dev.
#
# Takes the signed + notarised distribution package, refuses unless it
# passes the installer signature checks, and writes a flat release folder
# (normally dist/<version>/):
#   <outdir>/<pkg file>
#   <outdir>/latest.json
# On the server, latest.json goes to the docroot and the pkg to
# releases/<version>/ — the json's url field says so.
#
# One pkg serves everything: the menu ships inside the full pkg and always
# carries the same version, so the menu entry in latest.json points at the
# full pkg too (decided 2026-11-11; the separate menu pkg is gone).
#
# Environment:
#   VERSION / MENU_VERSION          (required; the Makefile passes them)
#   TAINER_DIST_PKG                 (default dist/tainer-dist-$VERSION.pkg)
#   RELEASE_NOTES / MENU_NOTES      (required, one paragraph each)
#   MENU_MIN_TAINER                 (default $VERSION)
#   SKIP_SIGNATURE_CHECK=1          TEST ONLY; any other value is refused.
set -euo pipefail

OUT="${1:-}"
if [[ -z "$OUT" ]]; then
    echo "usage: release-files.sh <outdir>" >&2
    exit 2
fi

# A refused or failed run must leave nothing uploadable behind.
case "$OUT" in ""|/|.|..|"$HOME") echo "release-files: unsafe output folder: $OUT" >&2; exit 2;; esac
rm -rf "$OUT"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="https://get.tainer.dev"
TEAM_ID="C8J8D9XJP6"

: "${VERSION:?VERSION is required}"
: "${MENU_VERSION:?MENU_VERSION is required}"
TAINER_PKG="${TAINER_DIST_PKG:-dist/tainer-dist-${VERSION}.pkg}"
MENU_MIN="${MENU_MIN_TAINER:-$VERSION}"

if [[ -z "${RELEASE_NOTES:-}" || -z "${MENU_NOTES:-}" ]]; then
    echo "release-files: set RELEASE_NOTES and MENU_NOTES (one paragraph each)" >&2
    exit 2
fi

if [[ -n "${SKIP_SIGNATURE_CHECK:-}" && "${SKIP_SIGNATURE_CHECK}" != "1" ]]; then
    echo "release-files: SKIP_SIGNATURE_CHECK must be exactly 1 (or unset); refusing" >&2
    exit 2
fi

if [[ ! -f "$TAINER_PKG" ]]; then
    echo "release-files: missing $TAINER_PKG (build it with make pkg-dist-notarised)" >&2
    exit 1
fi

check_signed() {
    local pkg="$1" sig
    sig=$(pkgutil --check-signature "$pkg" 2>&1) || { echo "release-files: $pkg is not signed" >&2; return 1; }
    grep -q "Developer ID Installer" <<<"$sig" || { echo "release-files: $pkg: not a Developer ID Installer signature" >&2; return 1; }
    grep -q "$TEAM_ID" <<<"$sig" || { echo "release-files: $pkg: team ID is not $TEAM_ID" >&2; return 1; }
    spctl -a -t install "$pkg" >/dev/null 2>&1 || { echo "release-files: $pkg: rejected by spctl (not notarised?)" >&2; return 1; }
}

if [[ "${SKIP_SIGNATURE_CHECK:-}" == "1" ]]; then
    echo "!!!!------------------------------------------------------------!!!!" >&2
    echo "WARNING: SKIP_SIGNATURE_CHECK=1 - signatures NOT verified. TEST ONLY." >&2
    echo "         Never upload the output of this run." >&2
    echo "!!!!------------------------------------------------------------!!!!" >&2
else
    check_signed "$TAINER_PKG"
fi

CONTROL_TAG=$(sed -n 's/^const controlImageVersion = "\(.*\)"$/\1/p' "$ROOT/pkg/tainer/pod/control.go")
if [[ -z "$CONTROL_TAG" ]]; then
    echo "release-files: could not read controlImageVersion from pkg/tainer/pod/control.go" >&2
    exit 1
fi

sha() { shasum -a 256 "$1" | awk '{print $1}'; }
# One paragraph: collapse whitespace, then escape for JSON.
jstr() { printf '%s' "$1" | tr '\n\t\r' '   ' | tr -d '\000-\037\177' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }

TAINER_SHA=$(sha "$TAINER_PKG")
if [[ -z "$TAINER_SHA" ]]; then
    echo "release-files: could not compute sha256" >&2
    rm -rf "$OUT"
    exit 1
fi

mkdir -p "$OUT"
cp "$TAINER_PKG" "$OUT/"
TN=$(basename "$TAINER_PKG")

cat > "$OUT/latest.json" <<JSON
{
  "tainer": {
    "version": "$VERSION",
    "url": "$BASE_URL/releases/$VERSION/$TN",
    "sha256": "$TAINER_SHA",
    "notes": "$(jstr "$RELEASE_NOTES")"
  },
  "menu": {
    "version": "$MENU_VERSION",
    "url": "$BASE_URL/releases/$VERSION/$TN",
    "sha256": "$TAINER_SHA",
    "notes": "$(jstr "$MENU_NOTES")",
    "minTainer": "$MENU_MIN"
  },
  "control": {
    "tag": "$CONTROL_TAG"
  }
}
JSON

if plutil -convert json -o /dev/null "$OUT/latest.json" 2>/dev/null; then :
elif command -v python3 >/dev/null && python3 -I -c 'import json,sys; json.load(open(sys.argv[1]))' "$OUT/latest.json" 2>/dev/null; then :
else
    echo "release-files: generated latest.json is not valid JSON" >&2
    rm -rf "$OUT"
    exit 1
fi

echo "Release files ready in: $OUT"
if [[ "${SKIP_SIGNATURE_CHECK:-}" == "1" ]]; then
    echo "TEST RUN - DO NOT UPLOAD this folder."
else
    echo "upload to get.tainer.dev: latest.json -> docroot, $TN -> releases/$VERSION/"
fi
