# Releasing tainer

This document covers building the two tainer installers:

- **`tainer-X.Y.Z.pkg`** — starter pack. Installs the tainer CLI plus
  everything cyberstack ships (cyberstackd, cyberstack-pf, gvproxy,
  vfkit, vmnet-helper, the Linux boot disk). One-click "I'm new, give me
  everything" install.
- **`tainer-only-X.Y.Z.pkg`** — CLI only. For users who already have
  cyberstack installed (via `cyberstack.pkg` from the cyber-stack repo).

Both ship to `/opt/tainer/bin/tainer` and create `/usr/local/bin/tainer`
as a symlink so the CLI lands on `PATH` out of the box.

## 1. Prerequisites

Same as cyberstack — see [`cyber-stack/docs/RELEASING.md`](../../cyber-stack/docs/RELEASING.md)
for the full walkthrough. In short:

- Developer ID **Application** + Developer ID **Installer** certs in your
  keychain, exposed via `$TAINER_SIGNING_IDENTITY` and
  `$TAINER_SIGNING_INSTALLER_IDENTITY`.
- A `notarytool` keychain profile (default name `tainer-notary`,
  override with `$TAINER_NOTARY_PROFILE`).
- A local checkout of `cyber-stack` at `../cyber-stack` (relative to
  this repo) — or pass `CS_REPO=/abs/path` on the `make` command line.

## 2. Per-release workflow

### 2.1 Bump version

Edit `Makefile`: `VERSION := X.Y.Z`. There's no in-code version constant
in tainer (yet); the Makefile is the only source.

### 2.2 Build, sign, notarise

```sh
# tainer-only.pkg — small, fast (~5s)
make pkg-only-notarised

# tainer.pkg starter pack — large, depends on cyberstack build (~3min)
CS_REPO=../cyber-stack make pkg-full-notarised
```

Both targets gracefully degrade when env vars are unset:

- No `$TAINER_SIGNING_IDENTITY` → ad-hoc sign with `-`. Pkg layout works
  but Gatekeeper rejects.
- No `$TAINER_SIGNING_INSTALLER_IDENTITY` → `pkg-only-signed` /
  `pkg-full-signed` errors out cleanly.
- No notarytool profile → `pkg-only-notarised` / `pkg-full-notarised`
  errors out at submission time.

### 2.3 Verify

```sh
make verify-pkg-only       # spctl + pkgutil for the small pkg
make verify-pkg-full       # same for the starter pack
```

### 2.4 Tag

```sh
git tag -a vX.Y.Z -m "tainer vX.Y.Z — release notes"
```

## 3. Targets cheat-sheet

| Target               | What it does                                              |
| -------------------- | --------------------------------------------------------- |
| `make build`         | Compiles `bin/tainer`                                     |
| `make sign`          | codesign with hardened runtime + entitlements             |
| `make verify-signatures` | codesign --display + --verify                         |
| `make pkg-only`      | pkgbuild `dist/tainer-only-X.Y.Z.pkg` (unsigned)          |
| `make pkg-only-signed`     | + productsign                                       |
| `make pkg-only-notarised`  | + notarytool submit + stapler                       |
| `make verify-pkg-only`     | pkgutil + spctl on the small pkg                    |
| `make pkg-full`      | Builds cyberstack pkg-root + layers tainer on top         |
| `make pkg-full-signed`     | + productsign                                       |
| `make pkg-full-notarised`  | + notarytool submit + stapler                       |
| `make verify-pkg-full`     | pkgutil + spctl on the starter pack                 |

## 4. Why two pkgs and not one with options?

We considered a multi-component `productbuild --distribution` pkg that
lets the user pick "tainer only" vs "everything". Two separate pkgs is
simpler because:

- The starter pack's audience is people who don't know they have a choice.
  They click `tainer.pkg` and expect it to work.
- The tainer-only audience is people who know exactly what they want and
  prefer the smaller download.
- Single-component pkgs install with zero UI clicks beyond
  "Continue → Install". A `productbuild` distribution adds a "Customise"
  step that confuses the starter-pack audience for no upside.

If we ever want a unified installer with a choice, swap `pkgbuild` for
`productbuild --distribution` and feed it both component pkgs.

## 5. Troubleshooting

See `cyber-stack/docs/RELEASING.md` — the failure modes are identical
since both pkgs share the same signing and notarisation pipeline.
