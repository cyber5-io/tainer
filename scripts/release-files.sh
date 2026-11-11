#!/bin/bash
# release-files.sh <channel-dir> - add this release to the local channel
# mirror (normally dist/releases) that get.tainer.dev is uploaded from.
#
# Takes the signed + notarised distribution package, refuses unless it
# passes the installer signature checks, and writes:
#   <channel-dir>/latest.json           (overwritten; the current channel file)
#   <channel-dir>/<version>/<pkg file>  (this release; older versions stay)
# On the server, latest.json goes to the docroot and each version folder to
# releases/<version>/ — the json's url field says so.
#
# One pkg serves everything: the menu ships inside the full pkg and always
# carries the same version (decided 2026-11-11; the separate menu pkg and
# the json's menu section are gone — old clients treat a missing menu
# section as "no menu update" and get the new menu from the full pkg).
#
# Environment:
#   VERSION                  (required; the Makefile passes it)
#   TAINER_DIST_PKG          (default dist/build/tainer-dist-$VERSION.pkg)
#   RELEASE_NOTES            (optional; defaults to a summary of the commits
#                             since the previous "build: <version>" commit)
#   SKIP_SIGNATURE_CHECK=1   TEST ONLY; any other value is refused.
set -euo pipefail

OUT="${1:-}"
if [[ -z "$OUT" ]]; then
    echo "usage: release-files.sh <channel-dir>" >&2
    exit 2
fi
case "$OUT" in ""|/|.|..|"$HOME") echo "release-files: unsafe channel folder: $OUT" >&2; exit 2;; esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="https://get.tainer.dev"
TEAM_ID="C8J8D9XJP6"

: "${VERSION:?VERSION is required}"
TAINER_PKG="${TAINER_DIST_PKG:-dist/build/tainer-dist-${VERSION}.pkg}"

# A refused or failed run must leave no half-made version behind, but never
# touches other versions already in the channel mirror.
VDIR="$OUT/$VERSION"
fail() { rm -rf "$VDIR" "$OUT/latest.json.new"; exit 1; }

if [[ -n "${SKIP_SIGNATURE_CHECK:-}" && "${SKIP_SIGNATURE_CHECK}" != "1" ]]; then
    echo "release-files: SKIP_SIGNATURE_CHECK must be exactly 1 (or unset); refusing" >&2
    exit 2
fi

if [[ ! -f "$TAINER_PKG" ]]; then
    echo "release-files: missing $TAINER_PKG (build it with make pkg-dist-notarised)" >&2
    exit 1
fi

# Notes: given, or summarised from the commits since the previous release's
# "build: <version>" commit (skipping build/chore/test/docs/ci subjects).
if [[ -z "${RELEASE_NOTES:-}" ]]; then
    prev=""
    while IFS=' ' read -r hash subject; do
        [[ "$subject" == "build: $VERSION"* ]] && continue
        prev="$hash"
        break
    done < <(git -C "$ROOT" log -n 50 --format='%H %s' --grep='^build: [0-9]' HEAD)
    if [[ -z "$prev" ]]; then
        echo "release-files: no previous build commit found; set RELEASE_NOTES" >&2
        exit 1
    fi
    RELEASE_NOTES="New in $VERSION: $(git -C "$ROOT" log --no-merges --format='%s' "$prev..HEAD" \
        | grep -vE '^(build|chore|test|docs|ci)(\(|:)' \
        | sed -E 's/^[a-z]+(\([a-z-]+\))?: //; s/ \(tain-[0-9]+\)$//' \
        | head -12 | paste -sd ';' - | sed 's/;/; /g')."
    if [[ "$RELEASE_NOTES" == "New in $VERSION: ." ]]; then
        echo "release-files: no commits to summarise; set RELEASE_NOTES" >&2
        exit 1
    fi
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
    fail
fi

rm -rf "$VDIR"
mkdir -p "$VDIR"
cp "$TAINER_PKG" "$VDIR/"
TN=$(basename "$TAINER_PKG")

cat > "$OUT/latest.json.new" <<JSON
{
  "tainer": {
    "version": "$VERSION",
    "url": "$BASE_URL/releases/$VERSION/$TN",
    "sha256": "$TAINER_SHA",
    "notes": "$(jstr "$RELEASE_NOTES")"
  },
  "control": {
    "tag": "$CONTROL_TAG"
  }
}
JSON

if plutil -convert json -o /dev/null "$OUT/latest.json.new" 2>/dev/null; then :
elif command -v python3 >/dev/null && python3 -I -c 'import json,sys; json.load(open(sys.argv[1]))' "$OUT/latest.json.new" 2>/dev/null; then :
else
    echo "release-files: generated latest.json is not valid JSON" >&2
    fail
fi
mv "$OUT/latest.json.new" "$OUT/latest.json"

echo "Release $VERSION ready in: $OUT"
echo "notes: $RELEASE_NOTES"
if [[ "${SKIP_SIGNATURE_CHECK:-}" == "1" ]]; then
    echo "TEST RUN - DO NOT UPLOAD this folder."
else
    echo "upload to get.tainer.dev: latest.json -> docroot, $VERSION/ -> releases/$VERSION/"
fi
