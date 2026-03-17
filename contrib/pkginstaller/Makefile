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

default: pkginstaller

tainer_version:
	make -B -C ../../ test/version/version

packagedir: tainer_version package_root Distribution welcome.html
	mkdir -p $(PACKAGE_DIR)
	cp -r Resources $(PACKAGE_DIR)/
	cp welcome.html $(PACKAGE_DIR)/Resources/
	cp Distribution $(PACKAGE_DIR)/
	cp -r scripts $(PACKAGE_DIR)/
	cp -r $(PACKAGE_ROOT) $(PACKAGE_DIR)/
	cp package.sh $(PACKAGE_DIR)/
	cd $(PACKAGE_DIR) && pkgbuild --analyze --root ./root component.plist
	../../test/version/version > $(PACKAGE_DIR)/VERSION
	echo -n $(ARCH) > $(PACKAGE_DIR)/ARCH
	cp ../../LICENSE $(PACKAGE_DIR)/Resources/LICENSE.txt
	cp vfkit.entitlements $(PACKAGE_DIR)/
	cp krunkit.entitlements $(PACKAGE_DIR)/

package_root: clean-pkgroot
	mkdir -p $(PACKAGE_ROOT)/tainer/bin $(PACKAGE_ROOT)/tainer/lib $(PACKAGE_ROOT)/tainer/templates
	cp $(HELPERS_DIR)/bin/gvproxy $(PACKAGE_ROOT)/tainer/bin/
	cp $(HELPERS_DIR)/bin/vfkit $(PACKAGE_ROOT)/tainer/bin/
	cp $(HELPERS_DIR)/bin/krunkit $(PACKAGE_ROOT)/tainer/bin/
	cp $(HELPERS_DIR)/lib/*.dylib $(PACKAGE_ROOT)/tainer/lib/
	cp -R ../../templates/* $(PACKAGE_ROOT)/tainer/templates/
	cp scripts/uninstall.sh $(PACKAGE_ROOT)/tainer/bin/uninstall.sh
	chmod a+x $(PACKAGE_ROOT)/tainer/bin/*

%: %.in tainer_version
	@sed -e 's/__VERSION__/'$(shell ../../test/version/version)'/g' $< >$@

pkginstaller: packagedir
	cd $(PACKAGE_DIR) && ./package.sh ..

_notarize: pkginstaller
	xcrun notarytool submit --apple-id $(NOTARIZE_USERNAME) --password $(NOTARIZE_PASSWORD) --team-id=$(NOTARIZE_TEAM) -f json --wait out/$(PKG_NAME)

notarize: _notarize
	xcrun stapler staple out/$(PKG_NAME)

.PHONY: clean clean-pkgroot
clean:
	rm -rf $(PACKAGE_ROOT) $(PACKAGE_DIR) out Distribution welcome.html ../../test/version/version

clean-pkgroot:
	rm -rf $(PACKAGE_ROOT) $(PACKAGE_DIR) Distribution welcome.html
