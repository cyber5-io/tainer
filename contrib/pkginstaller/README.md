## How to build

```sh
$ make ARCH=aarch64 NO_CODESIGN=1 pkginstaller

# or to create signed pkg
$ make ARCH=aarch64 CODESIGN_IDENTITY=<ID> PRODUCTSIGN_IDENTITY=<ID> pkginstaller

# or to prepare a signed and notarized pkg for release
$ make ARCH=aarch64 CODESIGN_IDENTITY=<ID> PRODUCTSIGN_IDENTITY=<ID> NOTARIZE_USERNAME=<appleID> NOTARIZE_PASSWORD=<appleID-password> NOTARIZE_TEAM=<team-id> notarize
```

The generated pkg will be written to `out/tainer-installer-macos-*.pkg`.
Currently the pkg installs `tainer`, `vfkit`, `gvproxy`, `krunkit`, and `tainer-mac-helper` to `/opt/tainer`

## Uninstalling

```sh
$ sudo rm -rf /opt/tainer
```
