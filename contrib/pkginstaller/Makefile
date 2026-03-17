SHELL := bash
GO ?= go

ARCH ?= aarch64
ifeq ($(ARCH), aarch64)
	GOARCH:=arm64
else
	GOARCH:=$(ARCH)
endif
HELPERS_DIR ?= helpers/darwin-$(GOARCH)
PACKAGE_DIR ?= out/packaging
PACKAGE_ROOT ?= root
PKG_NAME := tainer-installer-macos-$(GOARCH).pkg

# Extract Tainer product version from rawversion/version.go
TAINER_VERSION := $(shell grep 'const TainerVersion' ../../version/rawversion/version.go | sed 's/.*"\(.*\)"/\1/')

default: pkginstaller

packagedir: package_root Distribution welcome.html
	mkdir -p $(PACKAGE_DIR)
	cp -r Resources $(PACKAGE_DIR)/
	cp welcome.html $(PACKAGE_DIR)/Resources/
	cp Distribution $(PACKAGE_DIR)/
	cp -r scripts $(PACKAGE_DIR)/
	cp -r $(PACKAGE_ROOT) $(PACKAGE_DIR)/
	cp package.sh $(PACKAGE_DIR)/
	cd $(PACKAGE_DIR) && pkgbuild --analyze --root ./root component.plist
	echo -n $(TAINER_VERSION) > $(PACKAGE_DIR)/VERSION
	echo -n $(ARCH) > $(PACKAGE_DIR)/ARCH
	cp ../../LICENSE $(PACKAGE_DIR)/Resources/LICENSE.txt
	cp vfkit.entitlements $(PACKAGE_DIR)/
	cp krunkit.entitlements $(PACKAGE_DIR)/

package_root: clean-pkgroot
	mkdir -p $(PACKAGE_ROOT)/tainer/bin $(PACKAGE_ROOT)/tainer/lib $(PACKAGE_ROOT)/tainer/templates $(PACKAGE_ROOT)/tainer/certs
	cp $(HELPERS_DIR)/bin/gvproxy $(PACKAGE_ROOT)/tainer/bin/
	cp $(HELPERS_DIR)/bin/vfkit $(PACKAGE_ROOT)/tainer/bin/
	cp $(HELPERS_DIR)/bin/krunkit $(PACKAGE_ROOT)/tainer/bin/
	cp $(HELPERS_DIR)/lib/*.dylib $(PACKAGE_ROOT)/tainer/lib/
	cp -R ../../templates/* $(PACKAGE_ROOT)/tainer/templates/
	cp certs/tainer.me.crt $(PACKAGE_ROOT)/tainer/certs/
	cp certs/tainer.me.key $(PACKAGE_ROOT)/tainer/certs/
	cp scripts/uninstall.sh $(PACKAGE_ROOT)/tainer/bin/uninstall.sh
	chmod a+x $(PACKAGE_ROOT)/tainer/bin/*

%: %.in
	@sed -e 's/__VERSION__/$(TAINER_VERSION)/g' $< >$@

pkginstaller: packagedir
	cd $(PACKAGE_DIR) && ./package.sh ..

_notarize: pkginstaller
	xcrun notarytool submit --apple-id $(NOTARIZE_USERNAME) --password $(NOTARIZE_PASSWORD) --team-id=$(NOTARIZE_TEAM) -f json --wait out/$(PKG_NAME)

notarize: _notarize
	xcrun stapler staple out/$(PKG_NAME)

.PHONY: clean clean-pkgroot
clean:
	rm -rf $(PACKAGE_ROOT) $(PACKAGE_DIR) out Distribution welcome.html

clean-pkgroot:
	rm -rf $(PACKAGE_ROOT) $(PACKAGE_DIR) Distribution welcome.html
