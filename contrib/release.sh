#!/bin/bash
#
# release.sh — Build, sign, notarize, tag, and publish a tainer release.
#
# Usage:
#   ./contrib/release.sh              # build + release current version
#   ./contrib/release.sh --dry-run    # build everything, skip GitHub release
#
# Prerequisites:
#   - Developer ID certs in keychain
#   - tainer-notarize keychain profile configured
#   - gh CLI authenticated
#   - On main branch with clean working tree
#

set -euo pipefail

REPOROOT="$(cd "$(dirname "$0")/.." && pwd)"
DRY_RUN=0

if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
  echo "==> DRY RUN — will build but not create GitHub release"
fi

cd "$REPOROOT"

# --------------------------------------------------------------------------
# Pre-flight checks
# --------------------------------------------------------------------------

# Ensure clean working tree
if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  echo "ERROR: Working tree is dirty. Commit or stash changes first."
  exit 1
fi

# Ensure on main
branch=$(git branch --show-current)
if [[ "$branch" != "main" ]]; then
  echo "ERROR: Must be on main branch (currently on $branch)"
  exit 1
fi

# Read version
version=$(grep 'const TainerVersion' version/rawversion/version.go | sed 's/.*"\(.*\)".*/\1/')
tag="v${version}"

echo "==> Releasing tainer ${tag}"

# Check tag doesn't already exist
if git rev-parse "$tag" >/dev/null 2>&1; then
  echo "ERROR: Tag ${tag} already exists. Bump version first."
  exit 1
fi

# --------------------------------------------------------------------------
# Step 1: Build raw binaries (both architectures)
# --------------------------------------------------------------------------

outdir="${REPOROOT}/contrib/pkginstaller/out"
mkdir -p "$outdir"

echo ""
echo "==> Building arm64 binary..."
make -B GOARCH=arm64 tainer-remote
cp bin/darwin/tainer "$outdir/tainer-darwin-arm64"

echo ""
echo "==> Building amd64 binary..."
make -B GOARCH=amd64 tainer-remote
cp bin/darwin/tainer "$outdir/tainer-darwin-amd64"

# Verify architectures
arm64_arch=$(file "$outdir/tainer-darwin-arm64" | grep -o 'arm64\|x86_64' | tail -1)
amd64_arch=$(file "$outdir/tainer-darwin-amd64" | grep -o 'arm64\|x86_64' | tail -1)

if [[ "$arm64_arch" != "arm64" ]]; then
  echo "ERROR: arm64 binary has wrong architecture: $arm64_arch"
  exit 1
fi
if [[ "$amd64_arch" != "x86_64" ]]; then
  echo "ERROR: amd64 binary has wrong architecture: $amd64_arch"
  exit 1
fi

echo "==> Binaries verified: arm64=arm64, amd64=x86_64"

# --------------------------------------------------------------------------
# Step 2: Build pkg installers (signed + notarized)
# --------------------------------------------------------------------------

echo ""
echo "==> Building arm64 pkg installer..."
cp "$outdir/tainer-darwin-arm64" bin/darwin/tainer
ARCH=arm64 contrib/pkginstaller/package.sh

echo ""
echo "==> Building amd64 pkg installer..."
cp "$outdir/tainer-darwin-amd64" bin/darwin/tainer
ARCH=amd64 contrib/pkginstaller/package.sh

# --------------------------------------------------------------------------
# Step 3: Restore local arm64 binary
# --------------------------------------------------------------------------

cp "$outdir/tainer-darwin-arm64" bin/darwin/tainer
echo ""
echo "==> Restored local arm64 binary"

# --------------------------------------------------------------------------
# Step 4: Verify all assets
# --------------------------------------------------------------------------

echo ""
echo "==> Release assets:"
for f in \
  "$outdir/tainer-darwin-arm64" \
  "$outdir/tainer-darwin-amd64" \
  "$outdir/tainer-installer-macos-arm64.pkg" \
  "$outdir/tainer-installer-macos-amd64.pkg"; do
  if [[ ! -f "$f" ]]; then
    echo "ERROR: Missing asset: $f"
    exit 1
  fi
  size=$(du -h "$f" | cut -f1)
  echo "  ✓ $(basename "$f") ($size)"
done

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo ""
  echo "==> DRY RUN complete. Assets built at: $outdir"
  exit 0
fi

# --------------------------------------------------------------------------
# Step 5: Tag and push
# --------------------------------------------------------------------------

echo ""
echo "==> Tagging ${tag}..."
git tag "$tag"
git push origin main "$tag"

# --------------------------------------------------------------------------
# Step 6: Create GitHub release
# --------------------------------------------------------------------------

echo ""
echo "==> Creating GitHub release ${tag}..."

gh release create "$tag" \
  "$outdir/tainer-darwin-arm64" \
  "$outdir/tainer-darwin-amd64" \
  "$outdir/tainer-installer-macos-arm64.pkg" \
  "$outdir/tainer-installer-macos-amd64.pkg" \
  --title "$tag" \
  --notes "Release $tag — see commit log for details.

### Installers
- \`tainer-installer-macos-arm64.pkg\` — Apple Silicon (M1/M2/M3/M4)
- \`tainer-installer-macos-amd64.pkg\` — Intel Mac

### CLI update
Run \`tainer update core\` to update an existing installation."

echo ""
echo "==> Released: https://github.com/cyber5-io/tainer/releases/tag/${tag}"
