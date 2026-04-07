#!/bin/bash

set -euxo pipefail

BASEDIR=$(dirname "$0")
REPOROOT="${BASEDIR}/../.."
OUTPUT=${1:-${BASEDIR}/out}
CODESIGN_IDENTITY=${CODESIGN_IDENTITY:-"Developer ID Application: Cyber5 Internet Ltd (C8J8D9XJP6)"}
PRODUCTSIGN_IDENTITY=${PRODUCTSIGN_IDENTITY:-"Developer ID Installer: Cyber5 Internet Ltd (C8J8D9XJP6)"}
NO_CODESIGN=${NO_CODESIGN:-0}
NOTARIZE_PROFILE=${NOTARIZE_PROFILE:-"tainer-notarize"}
NOTARIZE=${NOTARIZE:-1}
HELPER_BINARIES_DIR="/opt/tainer/bin"
BUILD_ORIGIN="pkginstaller"

tmpBin="$(cd "${BASEDIR}" && pwd)/tmp-bin"

binDir="${BASEDIR}/root/tainer/bin"
libDir="${BASEDIR}/root/tainer/lib"
docDir="${BASEDIR}/root/tainer/docs/man/man1"

# Read version from source of truth
version=$(grep 'const TainerVersion' "${REPOROOT}/version/rawversion/version.go" | sed 's/.*"\(.*\)".*/\1/')
case "${ARCH:-arm64}" in
  aarch64|arm64) goArch=arm64 ;;
  x86_64|amd64)  goArch=amd64 ;;
  *)             goArch="${ARCH}" ;;
esac

# Generate Distribution and welcome.html from templates
sed "s/__VERSION__/${version}/g" "${BASEDIR}/Distribution.in" > "${BASEDIR}/Distribution"
sed "s/__VERSION__/${version}/g" "${BASEDIR}/welcome.html.in" > "${BASEDIR}/welcome.html"

function build_tainer() {
  pushd "${REPOROOT}"

  # Build docs if possible (requires GNU man/grep, skip on macOS)
  if make tainer-remote-darwin-docs 2>/dev/null; then
    mkdir -p "contrib/pkginstaller/out/packaging/${docDir}"
    cp -v docs/build/remote/darwin/*.1 "contrib/pkginstaller/out/packaging/${docDir}"
  else
    echo "Skipping man page generation (GNU tools not available)"
  fi

  make -B GOARCH="${goArch}" tainer-remote HELPER_BINARIES_DIR="${HELPER_BINARIES_DIR}" BUILD_ORIGIN="${BUILD_ORIGIN}"
  make -B GOARCH="${goArch}" tainer-mac-helper
  mkdir -p "${tmpBin}"
  cp bin/darwin/tainer "${tmpBin}/tainer-${goArch}"
  cp bin/darwin/tainer-mac-helper "${tmpBin}/tainer-mac-helper-${goArch}"

  popd
}

function sign() {
  local opts=""
  entitlements="${BASEDIR}/$(basename "$1").entitlements"
  if [ -f "${entitlements}" ]; then
      opts="--entitlements ${entitlements}"
  fi
  if [ ! "${NO_CODESIGN}" -eq "1" ]; then
      opts="$opts --options runtime"
  fi
  codesign --deep --sign "${CODESIGN_IDENTITY}" --timestamp --force ${opts} "$1"
}

build_tainer

# Copy built binaries into packaging root
cp "${tmpBin}/tainer-${goArch}" "${binDir}/tainer"
cp "${tmpBin}/tainer-mac-helper-${goArch}" "${binDir}/tainer-mac-helper"

# krunkit has hardcoded rpath /opt/podman/lib — can't patch (packed __LINKEDIT).
# Postinstall creates a symlink /opt/podman/lib -> /opt/tainer/lib as a workaround.

sign "${binDir}/tainer"
sign "${binDir}/tainer-mac-helper"
sign "${binDir}/gvproxy"
sign "${binDir}/vfkit"

# krunkit + dylibs only exist on arm64 (Apple Silicon)
if [ -f "${binDir}/krunkit" ]; then
  sign "${binDir}/krunkit"
fi
for dylib in libkrun-efi.dylib libvirglrenderer.1.dylib libepoxy.0.dylib libMoltenVK.dylib; do
  if [ -f "${libDir}/${dylib}" ]; then
    sign "${libDir}/${dylib}"
  fi
done

# Generate component plist
pkgbuild --analyze --root "${BASEDIR}/root" "${BASEDIR}/component.plist"

pkgbuild --identifier io.cyber5.tainer --version "${version}" \
  --scripts "${BASEDIR}/scripts" \
  --root "${BASEDIR}/root" \
  --install-location /opt \
  --component-plist "${BASEDIR}/component.plist" \
  "${OUTPUT}/tainer.pkg"

productbuild --distribution "${BASEDIR}/Distribution" \
  --resources "${BASEDIR}/Resources" \
  --package-path "${OUTPUT}" \
  "${OUTPUT}/tainer-unsigned.pkg"
rm "${OUTPUT}/tainer.pkg"

if [ ! "${NO_CODESIGN}" -eq "1" ]; then
  productsign --timestamp --sign "${PRODUCTSIGN_IDENTITY}" "${OUTPUT}/tainer-unsigned.pkg" "${OUTPUT}/tainer-installer-macos-${goArch}.pkg"
else
  mv "${OUTPUT}/tainer-unsigned.pkg" "${OUTPUT}/tainer-installer-macos-${goArch}.pkg"
fi
rm -f "${OUTPUT}/tainer-unsigned.pkg"

PKG_FILE="${OUTPUT}/tainer-installer-macos-${goArch}.pkg"

# Notarize and staple (only if signed)
if [ ! "${NO_CODESIGN}" -eq "1" ] && [ "${NOTARIZE}" -eq "1" ]; then
  echo "Notarizing ${PKG_FILE}..."
  xcrun notarytool submit "${PKG_FILE}" --keychain-profile "${NOTARIZE_PROFILE}" --wait
  xcrun stapler staple "${PKG_FILE}"
fi

echo "Built: ${PKG_FILE} (v${version})"
