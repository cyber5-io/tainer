//go:build darwin

package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledMenuReadsPlist(t *testing.T) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>Tainer Menu</string>
<key>CFBundleShortVersionString</key><string>1.2.3</string>
<key>CFBundleVersion</key><string>7</string>
</dict></plist>`
	p := filepath.Join(t.TempDir(), "Info.plist")
	if err := os.WriteFile(p, []byte(plist), 0644); err != nil {
		t.Fatal(err)
	}
	old := menuPlistPath
	menuPlistPath = p
	t.Cleanup(func() { menuPlistPath = old })
	if got := InstalledMenu(); got != "1.2.3" {
		t.Fatalf("got %q", got)
	}
	menuPlistPath = filepath.Join(t.TempDir(), "missing.plist")
	if got := InstalledMenu(); got != "" {
		t.Fatalf("absent: got %q", got)
	}
}
