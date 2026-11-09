package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const liveBase = `version: 2
project: { name: shop, type: nodejs, domain: shop.tainer.me }
runtime: { node: "22", database: none }
pod: { size: small }
`

func TestLiveDomainParsesAndValidates(t *testing.T) {
	m, err := ParseBytes([]byte(liveBase + "live:\n  domain: shop.example.com\n"))
	if err != nil {
		t.Fatalf("valid live.domain refused: %v", err)
	}
	if m.Live == nil || m.Live.Domain != "shop.example.com" {
		t.Fatalf("Live = %+v, want domain shop.example.com", m.Live)
	}

	m, err = ParseBytes([]byte(liveBase))
	if err != nil {
		t.Fatal(err)
	}
	if m.Live != nil {
		t.Errorf("no live block: Live = %+v, want nil", m.Live)
	}

	for _, bad := range []string{
		"live:\n  domain: \"not a domain!\"\n",
		"live:\n  domain: https://x.com\n",
		"live:\n  domain: \"\"\n",
		"live: {}\n",
	} {
		if _, err := ParseBytes([]byte(liveBase + bad)); err == nil {
			t.Errorf("accepted %q", bad)
		} else if !strings.Contains(err.Error(), "live.domain") {
			t.Errorf("%q: error should name live.domain: %v", bad, err)
		}
	}
}

func TestLiveBlockMergesFromOverlay(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(liveBase+"live: { domain: shop.example.com }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LocalOverlayName), []byte("live: { domain: staging.example.com }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if m.Live == nil || m.Live.Domain != "staging.example.com" {
		t.Errorf("overlay live.domain not merged: %+v", m.Live)
	}

	// Overlay adds a live block the base lacks.
	dir2 := t.TempDir()
	mustWrite(t, filepath.Join(dir2, FileName), []byte(liveBase))
	mustWrite(t, filepath.Join(dir2, LocalOverlayName), []byte("live: { domain: shop.example.com }\n"))
	m, err = Load(filepath.Join(dir2, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if m.Live == nil || m.Live.Domain != "shop.example.com" {
		t.Errorf("overlay-only live block not merged: %+v", m.Live)
	}

	// An overlay without a live block leaves the base's alone.
	dir3 := t.TempDir()
	mustWrite(t, filepath.Join(dir3, FileName), []byte(liveBase+"live: { domain: shop.example.com }\n"))
	mustWrite(t, filepath.Join(dir3, LocalOverlayName), []byte("pod: { size: large }\n"))
	m, err = Load(filepath.Join(dir3, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if m.Live == nil || m.Live.Domain != "shop.example.com" {
		t.Errorf("base live block lost: %+v", m.Live)
	}

	// The merged result is validated like tainer.yaml.
	dir4 := t.TempDir()
	mustWrite(t, filepath.Join(dir4, FileName), []byte(liveBase))
	mustWrite(t, filepath.Join(dir4, LocalOverlayName), []byte("live: { domain: \"bad domain\" }\n"))
	if _, err := Load(filepath.Join(dir4, FileName)); err == nil {
		t.Error("overlay with an invalid live.domain accepted")
	}
}

func TestSaveOmitsEmptyLiveBlock(t *testing.T) {
	dir := t.TempDir()
	m, err := ParseBytes([]byte(liveBase))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, FileName)
	if err := Save(m, p); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "live") {
		t.Errorf("absent live block written:\n%s", raw)
	}

	m.Live = &LiveConfig{Domain: "shop.example.com"}
	if err := Save(m, p); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p)
	if !strings.Contains(string(raw), "live:\n    domain: shop.example.com") {
		t.Errorf("live block not written:\n%s", raw)
	}
	back, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if back.Live == nil || back.Live.Domain != "shop.example.com" {
		t.Errorf("round trip lost live block: %+v", back.Live)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
