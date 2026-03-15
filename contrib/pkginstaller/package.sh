#!/bin/bash

set -euxo pipefail

BASEDIR=$(dirname "$0")
OUTPUT=$1
CODESIGN_IDENTITY=${CODESIGN_IDENTITY:--}
PRODUCTSIGN_IDENTITY=${PRODUCTSIGN_IDENTITY:-mock}
NO_CODESIGN=${NO_CODESIGN:-0}
HELPER_BINARIES_DIR="/opt/tainer/bin"
BUILD_ORIGIN="pkginstaller"

tmpBin="contrib/pkginstaller/tmp-bin"

binDir="${BASEDIR}/root/tainer/bin"
libDir="${BASEDIR}/root/tainer/lib"
docDir="${BASEDIR}/root/tainer/docs/man/man1"

version=$(cat "${BASEDIR}/VERSION")
arch=$(cat "${BASEDIR}/ARCH")

function build_tainer() {
  pushd "$1"

  make tainer-remote-darwin-docs
  mkdir -p "contrib/pkginstaller/out/packaging/${docDir}"
  cp -v docs/build/remote/darwin/*.1 "contrib/pkginstaller/out/packaging/${docDir}"

  case ${goArch} in
  arm64)
    build_tainer_arch ${goArch}
    cp "${tmpBin}/tainer-${goArch}"  "contrib/pkginstaller/out/packaging/${binDir}/tainer"
    cp "${tmpBin}/tainer-mac-helper-${goArch}" "contrib/pkginstaller/out/packaging/${binDir}/tainer-mac-helper"
    ;;
  *)
    echo -n "Unknown arch: ${goArch}"
    ;;
  esac

  popd
}

function build_tainer_arch(){
    make -B GOARCH="$1" tainer-remote HELPER_BINARIES_DIR="${HELPER_BINARIES_DIR}" BUILD_ORIGIN="${BUILD_ORIGIN}"
    make -B GOARCH="$1" tainer-mac-helper
    mkdir -p "${tmpBin}"
    cp bin/darwin/tainer "${tmpBin}/tainer-$1"
    cp bin/darwin/tainer-mac-helper "${tmpBin}/tainer-mac-helper-$1"
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

goArch="${arch}"
if [ "${goArch}" = aarch64 ]; then
  goArch=arm64
fi

build_tainer "../../../../"

sign "${binDir}/tainer"
sign "${binDir}/tainer-mac-helper"
sign "${binDir}/gvproxy"
sign "${binDir}/vfkit"

sign "${binDir}/krunkit"
sign "${libDir}/libkrun-efi.dylib"
sign "${libDir}/libvirglrenderer.1.dylib"
sign "${libDir}/libepoxy.0.dylib"
sign "${libDir}/libMoltenVK.dylib"

pkgbuild --identifier com.cyber5.tainer --version "${version}" \
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
