package certsync

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
)

// fakeConfigDir points config.BaseDir at a temp dir and returns the certs dir.
func fakeConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	certsDir := config.CertsDir()
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	return certsDir
}

func makeCertPair(t *testing.T, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "*.tainer.me"},
		DNSNames:     []string{"*.tainer.me", "tainer.me"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func writeStamp(t *testing.T, certsDir string, checkedAt time.Time) {
	t.Helper()
	out, _ := json.Marshal(Outcome{CheckedAt: checkedAt})
	if err := os.WriteFile(filepath.Join(certsDir, ".last-check"), out, 0644); err != nil {
		t.Fatal(err)
	}
}

func writeCert(t *testing.T, notAfter time.Time) (certPEM []byte) {
	t.Helper()
	certPEM, keyPEM := makeCertPair(t, notAfter)
	if err := os.WriteFile(config.CertFile(), certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.KeyFile(), keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return certPEM
}

func serveEndpoint(t *testing.T, certPEM, keyPEM []byte, notAfter time.Time) {
	t.Helper()
	sum := sha256.Sum256(certPEM)
	fp := `{"sha256":"` + hex.EncodeToString(sum[:]) + `","notAfter":"` + notAfter.UTC().Format(time.RFC3339) +
		`","serial":"7","generated":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	mux := http.NewServeMux()
	mux.HandleFunc("/fingerprint.json", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(fp)) })
	mux.HandleFunc("/fullchain.pem", func(w http.ResponseWriter, r *http.Request) { w.Write(certPEM) })
	mux.HandleFunc("/privkey.pem", func(w http.ResponseWriter, r *http.Request) { w.Write(keyPEM) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("TAINER_CERT_URL", srv.URL+"/")
}

func TestShouldAttemptFreshStamp(t *testing.T) {
	certsDir := fakeConfigDir(t)
	writeStamp(t, certsDir, time.Now().Add(-time.Hour))
	if shouldAttempt(time.Now()) {
		t.Error("stamp 1h old → should not attempt")
	}
}

func TestShouldAttemptOutsideWindow(t *testing.T) {
	certsDir := fakeConfigDir(t)
	writeStamp(t, certsDir, time.Now().Add(-48*time.Hour))
	writeCert(t, time.Now().Add(200*24*time.Hour))
	if shouldAttempt(time.Now()) {
		t.Error("cert valid 200 days → should not attempt")
	}
	// the stamp must NOT have been advanced by the check itself
	out, ok := ReadLastOutcome()
	if !ok || time.Since(out.CheckedAt) < 24*time.Hour {
		t.Error("outside-window check must not advance the stamp")
	}
}

func TestShouldAttemptInsideWindow(t *testing.T) {
	certsDir := fakeConfigDir(t)
	writeStamp(t, certsDir, time.Now().Add(-48*time.Hour))
	writeCert(t, time.Now().Add(10*24*time.Hour))
	if !shouldAttempt(time.Now()) {
		t.Error("cert expiring in 10 days → should attempt")
	}
}

func TestShouldAttemptMissingCert(t *testing.T) {
	fakeConfigDir(t)
	if !shouldAttempt(time.Now()) {
		t.Error("missing cert → should attempt")
	}
}

func TestDailyRefreshNoChange(t *testing.T) {
	fakeConfigDir(t)
	notAfter := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Second)
	certPEM := writeCert(t, notAfter)
	keyPEM, _ := os.ReadFile(config.KeyFile())
	serveEndpoint(t, certPEM, keyPEM, notAfter)

	reloads := 0
	DailyRefresh(context.Background(), func(context.Context) error { reloads++; return nil })

	if reloads != 0 {
		t.Error("no change → no reload")
	}
	out, ok := ReadLastOutcome()
	if !ok {
		t.Fatal("outcome not written")
	}
	if out.Refreshed || out.Err != "" {
		t.Errorf("outcome = %+v, want Refreshed=false Err=empty", out)
	}
}

func TestDailyRefreshInstallsAndReloads(t *testing.T) {
	fakeConfigDir(t)
	oldNotAfter := time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)
	writeCert(t, oldNotAfter)
	newNotAfter := time.Now().Add(89 * 24 * time.Hour).Truncate(time.Second)
	newCert, newKey := makeCertPair(t, newNotAfter)
	serveEndpoint(t, newCert, newKey, newNotAfter)

	reloads := 0
	DailyRefresh(context.Background(), func(context.Context) error { reloads++; return nil })

	if reloads != 1 {
		t.Errorf("reloads = %d, want 1", reloads)
	}
	got, _ := os.ReadFile(config.CertFile())
	if string(got) != string(newCert) {
		t.Error("cert file not replaced with new cert")
	}
	out, ok := ReadLastOutcome()
	if !ok || !out.Refreshed || out.Err != "" {
		t.Errorf("outcome = %+v, want Refreshed=true", out)
	}
}

func TestDailyRefreshServerDown(t *testing.T) {
	fakeConfigDir(t)
	oldCert := writeCert(t, time.Now().Add(5*24*time.Hour))
	t.Setenv("TAINER_CERT_URL", "http://127.0.0.1:1/") // nothing listens

	DailyRefresh(context.Background(), func(context.Context) error { t.Error("reload must not run"); return nil })

	got, _ := os.ReadFile(config.CertFile())
	if string(got) != string(oldCert) {
		t.Error("cert must be untouched on failure")
	}
	out, ok := ReadLastOutcome()
	if !ok || out.Err == "" {
		t.Errorf("outcome = %+v, want Err set", out)
	}
}
