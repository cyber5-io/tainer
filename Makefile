.PHONY: build sign menu-app verify-signatures test clean \
        pkg-only-root pkg-only pkg-only-signed pkg-only-notarised \
        pkg-full-root pkg-full pkg-full-signed pkg-full-notarised \
        pkg-dist pkg-dist-signed pkg-dist-notarised \
        pkg-dist-only pkg-dist-only-signed pkg-dist-only-notarised \
        verify-pkg-only verify-pkg-full

# Single source of truth for the binary version.
VERSION := 1.2.0
LDFLAGS := -X main.version=$(VERSION)

BIN_DIR := bin
DIST_DIR := dist

# Source of the *.tainer.me wildcard cert bundled into the installers. The
# first run copies it into ~/.config/tainer/certs so Caddy can serve
# browser-trusted HTTPS with no prompt. Override with TAINER_CERT_DIR=/abs/path.
TAINER_CERT_DIR ?= $(HOME)/.config/tainer/certs

# bundle-certs,<pkg-root> — drop the wildcard cert+key into a pkg staging
# root at opt/tainer/share/tainer/certs. The key ships in the pkg (mode 0644
# so the unprivileged first-run copy can read it); acceptable because
# *.tainer.me only ever resolves to loopback for local dev.
define bundle-certs
	@if [ ! -f "$(TAINER_CERT_DIR)/tainer.me.crt" ] || [ ! -f "$(TAINER_CERT_DIR)/tainer.me.key" ]; then \
		echo "pkg: missing tainer.me cert/key in $(TAINER_CERT_DIR) (set TAINER_CERT_DIR=/abs/path)" >&2; \
		exit 1; \
	fi
	mkdir -p $(1)/opt/tainer/share/tainer/certs
	install -m 0644 $(TAINER_CERT_DIR)/tainer.me.crt $(1)/opt/tainer/share/tainer/certs/
	install -m 0644 $(TAINER_CERT_DIR)/tainer.me.key $(1)/opt/tainer/share/tainer/certs/
endef

# Default target — builds the host CLI.
build: $(BIN_DIR)/tainer

# FORCE prerequisite so this always re-runs. Without it, make treats an
# existing bin/tainer as up-to-date (the target has no source prereqs) and
# skips go build — which silently ships a stale binary through sign/pkg.
# Go's build cache makes a no-op recompile near-instant.
$(BIN_DIR)/tainer: FORCE
	@mkdir -p $(BIN_DIR)
	go build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/tainer ./cmd/tainer

FORCE:

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
	$(call bundle-certs,$(PKG_ONLY_ROOT))
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
NOTARY_PROFILE := $(if $(TAINER_NOTARY_PROFILE),$(TAINER_NOTARY_PROFILE),tainer-notarize)

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

# ---------------------------------------------------------------------------
# Distribution wrapper — the brandable installer.
#
# pkgbuild output is a bare component package: Installer.app shows the
# generic grey flow for it, no branding hooks at all. productbuild
# wraps a component in a distribution archive whose Distribution XML
# carries the brand surface: background image (light + dark),
# welcome/conclusion HTML panes, window title. Assets live in
# packaging/installer/resources; the XML template has @VERSION@ and
# @COMPONENT_PKG@ stamped at wrap time.
#
# pkg-dist wraps the FULL starter pack (tainer.pkg). Preview without
# signing: `make pkg-dist` then open dist/tainer-dist-$(VERSION).pkg.
# ---------------------------------------------------------------------------
PKG_DIST_NAME := tainer-dist-$(VERSION).pkg
PKG_DIST_OUT  := $(DIST_DIR)/$(PKG_DIST_NAME)
PKG_DIST_ONLY_NAME := tainer-only-dist-$(VERSION).pkg
PKG_DIST_ONLY_OUT  := $(DIST_DIR)/$(PKG_DIST_ONLY_NAME)

pkg-dist: pkg-full
	sed -e 's/@VERSION@/$(VERSION)/' -e 's/@COMPONENT_PKG@/$(PKG_FULL_NAME)/' \
		packaging/installer/distribution.xml > $(DIST_DIR)/distribution.xml
	productbuild \
		--distribution $(DIST_DIR)/distribution.xml \
		--resources packaging/installer/resources \
		--package-path $(DIST_DIR) \
		$(PKG_DIST_OUT)
	@echo "Built $(PKG_DIST_OUT) (unsigned distribution pkg)"

pkg-dist-only: pkg-only
	sed -e 's/@VERSION@/$(VERSION)/' -e 's/@COMPONENT_PKG@/$(PKG_ONLY_NAME)/' \
		-e 's/io.cyber5.tainer"/io.cyber5.tainer.only"/g' \
		-e 's/id="io.cyber5.tainer"/id="io.cyber5.tainer.only"/g' \
		packaging/installer/distribution.xml > $(DIST_DIR)/distribution-only.xml
	productbuild \
		--distribution $(DIST_DIR)/distribution-only.xml \
		--resources packaging/installer/resources \
		--package-path $(DIST_DIR) \
		$(PKG_DIST_ONLY_OUT)
	@echo "Built $(PKG_DIST_ONLY_OUT) (unsigned distribution pkg)"

pkg-dist-only-signed: pkg-dist-only
	@if [ -z "$(INSTALLER_SIGN_IDENTITY)" ]; then \
		echo "set TAINER_SIGNING_INSTALLER_IDENTITY" >&2; exit 1; \
	fi
	productsign --sign "$(INSTALLER_SIGN_IDENTITY)" \
		$(PKG_DIST_ONLY_OUT) $(PKG_DIST_ONLY_OUT).signed
	mv $(PKG_DIST_ONLY_OUT).signed $(PKG_DIST_ONLY_OUT)

pkg-dist-only-notarised: pkg-dist-only-signed
	xcrun notarytool submit $(PKG_DIST_ONLY_OUT) \
		--keychain-profile "$(NOTARY_PROFILE)" --wait
	xcrun stapler staple $(PKG_DIST_ONLY_OUT)

pkg-dist-signed: pkg-dist
	@if [ -z "$(INSTALLER_SIGN_IDENTITY)" ]; then \
		echo "set TAINER_SIGNING_INSTALLER_IDENTITY" >&2; exit 1; \
	fi
	productsign --sign "$(INSTALLER_SIGN_IDENTITY)" \
		$(PKG_DIST_OUT) $(PKG_DIST_OUT).signed
	mv $(PKG_DIST_OUT).signed $(PKG_DIST_OUT)

pkg-dist-notarised: pkg-dist-signed
	xcrun notarytool submit $(PKG_DIST_OUT) \
		--keychain-profile "$(NOTARY_PROFILE)" --wait
	xcrun stapler staple $(PKG_DIST_OUT)

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

# ---------------------------------------------------------------------------
# Tainer Menu — the menu-bar app bundle (LSUIElement agent). Built into
# an .app so the installer can drop it in /Applications and a LaunchAgent
# can start it at login. Signed with the same identity/hardened-runtime
# as the CLI so it rides the one notarisation chain.
# ---------------------------------------------------------------------------
MENU_APP_NAME := Tainer Menu.app
MENU_APP      := $(DIST_DIR)/$(MENU_APP_NAME)
MENU_BIN      := $(BIN_DIR)/tainer-menu

menu-app:
	@mkdir -p $(BIN_DIR) $(DIST_DIR)
	go build -ldflags '$(LDFLAGS)' -o $(MENU_BIN) ./cmd/tainer-menu
	rm -rf "$(MENU_APP)"
	mkdir -p "$(MENU_APP)/Contents/MacOS" "$(MENU_APP)/Contents/Resources"
	install -m 0755 $(MENU_BIN) "$(MENU_APP)/Contents/MacOS/tainer-menu"
	install -m 0644 packaging/menu/tainer-menu.icns "$(MENU_APP)/Contents/Resources/tainer-menu.icns"
	sed 's/@VERSION@/$(VERSION)/g' packaging/menu/Info.plist > "$(MENU_APP)/Contents/Info.plist"
	@if [ "$(SIGN_IDENTITY)" = "-" ]; then \
		echo "warning: TAINER_SIGNING_IDENTITY unset, ad-hoc signing menu app (NOT notarisable)"; \
	fi
	codesign $(SIGN_FLAGS_BASE) --sign "$(SIGN_IDENTITY)" "$(MENU_APP)"
	@echo "Built $(MENU_APP)"

# Builds cyberstack's pkg-root via its own Makefile, then copies the
# tree into our staging area + drops the tainer CLI on top.
pkg-full-root: sign menu-app
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
	# Bundle the *.tainer.me wildcard cert (first run provisions HTTPS).
	$(call bundle-certs,$(PKG_FULL_ROOT))
	# Menu-bar app in /Applications + a login agent to start it.
	mkdir -p "$(PKG_FULL_ROOT)/Applications"
	cp -R "$(MENU_APP)" "$(PKG_FULL_ROOT)/Applications/"
	mkdir -p $(PKG_FULL_ROOT)/Library/LaunchAgents
	install -m 0644 packaging/menu/io.cyber5.tainer.menu.plist \
		$(PKG_FULL_ROOT)/Library/LaunchAgents/io.cyber5.tainer.menu.plist
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
		--component-plist packaging/installer/pkg-full-components.plist \
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
