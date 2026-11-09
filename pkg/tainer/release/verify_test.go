package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

func fakeRunner(sig string, spctlErr error) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "pkgutil":
			return []byte(sig), nil
		case "spctl":
			return nil, spctlErr
		}
		return nil, errors.New("unexpected " + name)
	}
}

const goodSig = "Package \"x.pkg\":\n   Status: signed by a developer certificate issued by Apple\n   Certificate Chain:\n    1. Developer ID Installer: Cyber5 IO Ltd (C8J8D9XJP6)\n"

func TestVerifyPkgRequiresCyber5Team(t *testing.T) {
	ctx := context.Background()
	if err := VerifyPkg(ctx, fakeRunner(goodSig, nil), "x.pkg"); err != nil {
		t.Fatalf("good: %v", err)
	}
	other := strings.ReplaceAll(goodSig, "C8J8D9XJP6", "AAAAAAAAAA")
	if err := VerifyPkg(ctx, fakeRunner(other, nil), "x.pkg"); err == nil || !strings.Contains(err.Error(), "Cyber5") {
		t.Fatalf("other team: %v", err)
	}
	if err := VerifyPkg(ctx, fakeRunner(goodSig, errors.New("rejected")), "x.pkg"); err == nil || !strings.Contains(err.Error(), "notarised") {
		t.Fatalf("spctl: %v", err)
	}
}
