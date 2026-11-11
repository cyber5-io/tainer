package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// downloadTimeout bounds a whole package download (packages are ~100 MB).
var downloadTimeout = 10 * time.Minute

// Runner runs an external command and returns its combined output. It is
// the test seam for pkgutil/spctl.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner is the default Runner.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Download streams the artifact into dir (a private directory made by the
// caller), hashing as it writes. On a checksum mismatch the file is
// removed and an error returned.
func Download(ctx context.Context, a Artifact, dir string) (string, error) {
	return DownloadWithProgress(ctx, a, dir, nil)
}

// DownloadWithProgress is Download reporting the bytes written so far and
// the total (-1 when the server sends no length): once before the first
// byte, then after every write. report may be nil.
func DownloadWithProgress(ctx context.Context, a Artifact, dir string, report func(done, total int64)) (string, error) {
	if !strings.HasPrefix(a.URL, "https://") && os.Getenv("TAINER_RELEASE_URL") == "" {
		return "", errors.New("the download isn't from a secure address")
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("the download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the download failed (HTTP %d)", resp.StatusCode)
	}
	// Fixed name: the URL must never influence what pkgutil prints.
	dest := filepath.Join(dir, "tainer-update.pkg")
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h, newProgressWriter(resp.ContentLength, report)), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dest)
		return "", fmt.Errorf("the download failed: %w", err)
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		os.Remove(dest)
		return "", errors.New("the download didn't match its checksum")
	}
	return dest, nil
}

var leafCert = regexp.MustCompile(`^1\. Developer ID Installer: .+ \(C8J8D9XJP6\)$`)

// VerifyPkg checks the package's leaf certificate is Cyber5's Developer ID
// Installer certificate and that Gatekeeper accepts it as notarised.
func VerifyPkg(ctx context.Context, run Runner, pkg string) error {
	out, err := run(ctx, "pkgutil", "--check-signature", pkg)
	signed := false
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if leafCert.MatchString(strings.TrimSpace(line)) {
				signed = true
				break
			}
		}
	}
	if !signed {
		return errors.New("the download isn't signed by Cyber5")
	}
	out, err = run(ctx, "spctl", "-a", "-vv", "-t", "install", pkg)
	if err != nil || !strings.Contains(string(out), "source=Notarized Developer ID") {
		return errors.New("the download isn't notarised by Apple")
	}
	return nil
}

// progressWriter counts what passes through it and reports the running
// total.
type progressWriter struct {
	done, total int64
	report      func(done, total int64)
}

func newProgressWriter(total int64, report func(done, total int64)) *progressWriter {
	w := &progressWriter{total: total, report: report}
	if report != nil {
		report(0, total)
	}
	return w
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	if w.report != nil {
		w.report(w.done, w.total)
	}
	return len(p), nil
}
