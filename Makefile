.PHONY: build sign verify-signatures test clean \
        pkg-only-root pkg-only pkg-only-signed pkg-only-notarised \
        pkg-full-root pkg-full pkg-full-signed pkg-full-notarised \
        verify-pkg-only verify-pkg-full

# Single source of truth for the binary version.
VERSION := 0.9.0-dev
LDFLAGS := -X main.version=$(VERSION)

BIN_DIR := bin
DIST_DIR := dist

# Default target — builds the host CLI.
build: $(BIN_DIR)/tainer

$(BIN_DIR)/tainer:
	@mkdir -p $(BIN_DIR)
	go build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/tainer ./cmd/tainer

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

# ---------------------------------------------------------------------------
# Code signing for the tainer CLI. Matches cyberstack's pattern: real
# identity from $TAINER_SIGNING_IDENTITY when set, ad-hoc "-" otherwise.
# The CLI gets the same hardened-runtime treatment as cyberstackd so the
# notarisation chain stays consistent across both .pkgs.
# ---------------------------------------------------------------------------
SIGN_IDENTITY := $(if $(TAINER_SIGNING_IDENTITY),$(TAINER_SIGNING_IDENTITY),-)
SIGN_FLAGS_BASE := --force --options runtime --timestamp
ENT_DIR := packaging/entitlements

sign: build
	@if [ "$(SIGN_IDENTITY)" = "-" ]; then \
		echo "warning: TAINER_SIGNING_IDENTITY unset, ad-hoc signing (NOT notarisable)"; \
	fi
	codesign $(SIGN_FLAGS_BASE) --sign "$(SIGN_IDENTITY)" \
		--entitlements $(ENT_DIR)/tainer.entitlements.plist \
		$(BIN_DIR)/tainer

verify-signatures:
	@codesign --display --verbose=2 $(BIN_DIR)/tainer 2>&1 | \
		grep -E '^(Identifier|Authority|TeamIdentifier|Format)' || true
	@codesign --verify --strict --verbose=2 $(BIN_DIR)/tainer 2>&1 | tail -3

# ---------------------------------------------------------------------------
# tainer-only.pkg — minimal installer.
#
# Just installs the tainer CLI at /opt/tainer/bin/tainer and symlinks
# /usr/local/bin/tainer to it. For users who already have cyberstack
# installed via cyberstack.pkg.
# ---------------------------------------------------------------------------
PKG_ONLY_ROOT     := $(DIST_DIR)/pkg-only-root
PKG_ONLY_SCRIPTS  := $(DIST_DIR)/pkg-only-scripts
PKG_ONLY_NAME     := tainer-only-$(VERSION).pkg
PKG_ONLY_OUT      := $(DIST_DIR)/$(PKG_ONLY_NAME)
PKG_ONLY_IDENTIFIER := io.cyber5.tainer.only

pkg-only-root: sign
	rm -rf $(PKG_ONLY_ROOT) $(PKG_ONLY_SCRIPTS)
	mkdir -p $(PKG_ONLY_ROOT)/opt/tainer/bin
	mkdir -p $(PKG_ONLY_SCRIPTS)
	install -m 0755 $(BIN_DIR)/tainer $(PKG_ONLY_ROOT)/opt/tainer/bin/
	install -m 0755 packaging/scripts/postinstall-only $(PKG_ONLY_SCRIPTS)/postinstall
	@echo "pkg-only-root assembled at $(PKG_ONLY_ROOT)"

pkg-only: pkg-only-root
	mkdir -p $(DIST_DIR)
	pkgbuild \
		--root $(PKG_ONLY_ROOT) \
		--scripts $(PKG_ONLY_SCRIPTS) \
		--identifier $(PKG_ONLY_IDENTIFIER) \
		--version $(VERSION) \
		--install-location / \
		$(PKG_ONLY_OUT)
	@echo "Built $(PKG_ONLY_OUT) (unsigned)"

# Distinct env var from the application identity — Apple issues two
# separate Developer ID certificates per team. productsign needs the
# Installer one; codesign uses the Application one.
INSTALLER_SIGN_IDENTITY := $(TAINER_SIGNING_INSTALLER_IDENTITY)
NOTARY_PROFILE := $(if $(TAINER_NOTARY_PROFILE),$(TAINER_NOTARY_PROFILE),tainer-notary)

pkg-only-signed: pkg-only
	@if [ -z "$(INSTALLER_SIGN_IDENTITY)" ]; then \
		echo "set TAINER_SIGNING_INSTALLER_IDENTITY" >&2; exit 1; \
	fi
	productsign --sign "$(INSTALLER_SIGN_IDENTITY)" \
		$(PKG_ONLY_OUT) $(PKG_ONLY_OUT).signed
	mv $(PKG_ONLY_OUT).signed $(PKG_ONLY_OUT)

pkg-only-notarised: pkg-only-signed
	xcrun notarytool submit $(PKG_ONLY_OUT) \
		--keychain-profile "$(NOTARY_PROFILE)" --wait
	xcrun stapler staple $(PKG_ONLY_OUT)

verify-pkg-only:
	@pkgutil --check-signature $(PKG_ONLY_OUT) || true
	@echo
	@spctl --assess --type install --verbose=2 $(PKG_ONLY_OUT) 2>&1 || true
	@echo
	@pkgutil --payload-files $(PKG_ONLY_OUT)

# ---------------------------------------------------------------------------
# tainer.pkg — starter pack.
#
# Embeds everything cyberstack.pkg ships PLUS the tainer CLI and a
# combined postinstall. The cyberstack pkg-root tree is pulled from a
# sibling checkout of the cyber-stack repo (override the path with
# $CYBERSTACK_REPO if your layout differs).
#
# Why we don't make this a multi-component .pkg: a single component
# keeps Installer.app's UI to one click. A user who wants "cyberstack
# only" installs cyberstack.pkg directly from the cyber-stack repo.
# ---------------------------------------------------------------------------
CYBERSTACK_REPO  := $(if $(CS_REPO),$(CS_REPO),../cyber-stack)
PKG_FULL_ROOT    := $(DIST_DIR)/pkg-full-root
PKG_FULL_SCRIPTS := $(DIST_DIR)/pkg-full-scripts
PKG_FULL_NAME    := tainer-$(VERSION).pkg
PKG_FULL_OUT     := $(DIST_DIR)/$(PKG_FULL_NAME)
PKG_FULL_IDENTIFIER := io.cyber5.tainer

# Builds cyberstack's pkg-root via its own Makefile, then copies the
# tree into our staging area + drops the tainer CLI on top.
pkg-full-root: sign
	@if [ ! -d "$(CYBERSTACK_REPO)" ]; then \
		echo "pkg-full-root: CYBERSTACK_REPO=$(CYBERSTACK_REPO) is not a directory" >&2; \
		echo "  set CS_REPO=/abs/path/to/cyber-stack or 'ln -s' a sibling checkout" >&2; \
		exit 1; \
	fi
	@echo "==> building cyberstack pkg-root..."
	$(MAKE) -C $(CYBERSTACK_REPO) pkg-root
	rm -rf $(PKG_FULL_ROOT) $(PKG_FULL_SCRIPTS)
	mkdir -p $(PKG_FULL_ROOT) $(PKG_FULL_SCRIPTS)
	# Start from cyberstack's tree (so we get cyberstackd, gvproxy,
	# vfkit, vmnet-helper, boot disk, install-cyberstack-pf.sh).
	cp -R $(CYBERSTACK_REPO)/dist/pkg-root/. $(PKG_FULL_ROOT)/
	# Layer the tainer CLI on top.
	install -m 0755 $(BIN_DIR)/tainer $(PKG_FULL_ROOT)/opt/tainer/bin/
	# Combined postinstall — does cyberstack's setup PLUS the symlink.
	install -m 0755 packaging/scripts/postinstall-full $(PKG_FULL_SCRIPTS)/postinstall
	@echo "pkg-full-root assembled at $(PKG_FULL_ROOT)"
	@du -sh $(PKG_FULL_ROOT)/opt/* 2>/dev/null

pkg-full: pkg-full-root
	mkdir -p $(DIST_DIR)
	pkgbuild \
		--root $(PKG_FULL_ROOT) \
		--scripts $(PKG_FULL_SCRIPTS) \
		--identifier $(PKG_FULL_IDENTIFIER) \
		--version $(VERSION) \
		--install-location / \
		$(PKG_FULL_OUT)
	@echo "Built $(PKG_FULL_OUT) (unsigned)"

pkg-full-signed: pkg-full
	@if [ -z "$(INSTALLER_SIGN_IDENTITY)" ]; then \
		echo "set TAINER_SIGNING_INSTALLER_IDENTITY" >&2; exit 1; \
	fi
	productsign --sign "$(INSTALLER_SIGN_IDENTITY)" \
		$(PKG_FULL_OUT) $(PKG_FULL_OUT).signed
	mv $(PKG_FULL_OUT).signed $(PKG_FULL_OUT)

pkg-full-notarised: pkg-full-signed
	xcrun notarytool submit $(PKG_FULL_OUT) \
		--keychain-profile "$(NOTARY_PROFILE)" --wait
	xcrun stapler staple $(PKG_FULL_OUT)

verify-pkg-full:
	@pkgutil --check-signature $(PKG_FULL_OUT) || true
	@echo
	@spctl --assess --type install --verbose=2 $(PKG_FULL_OUT) 2>&1 || true
	@echo
	@pkgutil --payload-files $(PKG_FULL_OUT) | sort
