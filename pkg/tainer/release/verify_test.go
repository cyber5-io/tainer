package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func serve(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TAINER_RELEASE_URL", srv.URL)
	return srv.URL + "/tainer.pkg"
}

func TestDownloadChecksSHA256(t *testing.T) {
	body := "pkg-bytes"
	sum := sha256.Sum256([]byte(body))
	url := serve(t, body)

	dir := t.TempDir()
	path, err := Download(context.Background(), Artifact{URL: url, SHA256: hex.EncodeToString(sum[:])}, dir)
	if err != nil {
		t.Fatalf("good hash: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Fatalf("file content %q", got)
	}

	dir2 := t.TempDir()
	_, err = Download(context.Background(), Artifact{URL: url, SHA256: strings.Repeat("0", 64)}, dir2)
	if err == nil || !strings.Contains(err.Error(), "didn't match its checksum") {
		t.Fatalf("bad hash error: %v", err)
	}
	if entries, _ := os.ReadDir(dir2); len(entries) != 0 {
		t.Fatalf("bad download left files: %v", entries)
	}
}

func TestDownloadRefusesNonHTTPS(t *testing.T) {
	t.Setenv("TAINER_RELEASE_URL", "")
	_, err := Download(context.Background(), Artifact{URL: "http://example.com/x.pkg", SHA256: "00"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "secure") {
		t.Fatalf("expected refusal, got %v", err)
	}
}

func fakeRunner(sig string, sigErr error, spctl string, spctlErr error) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "pkgutil":
			return []byte(sig), sigErr
		case "spctl":
			return []byte(spctl), spctlErr
		}
		return nil, errors.New("unexpected " + name)
	}
}

const goodSig = "Package \"x.pkg\":\n   Status: signed by a developer certificate issued by Apple\n   Certificate Chain:\n    1. Developer ID Installer: Cyber5 IO Ltd (C8J8D9XJP6)\n       SHA256 Fingerprint: AA\n    2. Developer ID Certification Authority\n"
const goodSpctl = "x.pkg: accepted\nsource=Notarized Developer ID\n"

func TestVerifyPkgRequiresCyber5Team(t *testing.T) {
	ctx := context.Background()
	if err := VerifyPkg(ctx, fakeRunner(goodSig, nil, goodSpctl, nil), "x.pkg"); err != nil {
		t.Fatalf("good: %v", err)
	}
	other := strings.ReplaceAll(goodSig, "C8J8D9XJP6", "AAAAAAAAAA")
	if err := VerifyPkg(ctx, fakeRunner(other, nil, goodSpctl, nil), "x.pkg"); err == nil || !strings.Contains(err.Error(), "Cyber5") {
		t.Fatalf("other team: %v", err)
	}
	if err := VerifyPkg(ctx, fakeRunner(goodSig, nil, goodSpctl, errors.New("rejected")), "x.pkg"); err == nil || !strings.Contains(err.Error(), "notarised") {
		t.Fatalf("spctl: %v", err)
	}
}

func TestVerifyPkgIgnoresPathAndOtherLines(t *testing.T) {
	sig := "Package \"/tmp/Developer ID Installer C8J8D9XJP6.pkg\":\n   Certificate Chain:\n    1. Developer ID Installer: Other Ltd (AAAAAAAAAA)\n    2. Developer ID Installer C8J8D9XJP6\n"
	err := VerifyPkg(context.Background(), fakeRunner(sig, nil, goodSpctl, nil), "x.pkg")
	if err == nil || !strings.Contains(err.Error(), "Cyber5") {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyPkgUnsignedFails(t *testing.T) {
	err := VerifyPkg(context.Background(), fakeRunner("Status: no signature\n", errors.New("exit 1"), goodSpctl, nil), "x.pkg")
	if err == nil || !strings.Contains(err.Error(), "Cyber5") {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyPkgGatekeeperDisabledFails(t *testing.T) {
	err := VerifyPkg(context.Background(), fakeRunner(goodSig, nil, "x.pkg: accepted\noverride=security disabled\n", nil), "x.pkg")
	if err == nil || !strings.Contains(err.Error(), "notarised") {
		t.Fatalf("got %v", err)
	}
}

func TestDownloadUsesFixedName(t *testing.T) {
	body := "b"
	sum := sha256.Sum256([]byte(body))
	url := serve(t, body)[:len(serve(t, body))-len("tainer.pkg")] + "Developer%20ID%20Installer%20C8J8D9XJP6.pkg"
	p, err := Download(context.Background(), Artifact{URL: url, SHA256: hex.EncodeToString(sum[:])}, t.TempDir())
	if err != nil || filepath.Base(p) != "tainer-update.pkg" {
		t.Fatalf("path %q err %v", p, err)
	}
}
