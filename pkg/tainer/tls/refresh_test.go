package tls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// makeCertPair generates a self-signed ECDSA cert for dnsNames expiring
// at notAfter, returning PEM-encoded cert and key.
func makeCertPair(t *testing.T, dnsNames []string, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: dnsNames[0]},
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// serveFiles returns a test server whose baseURL serves the given bodies.
func serveFiles(t *testing.T, fingerprint, fullchain, privkey []byte) string {
	t.Helper()
	mux := http.NewServeMux()
	serve := func(path string, body []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Write(body)
		})
	}
	serve("/tainer.me/fingerprint.json", fingerprint)
	serve("/tainer.me/fullchain.pem", fullchain)
	serve("/tainer.me/privkey.pem", privkey)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/tainer.me/"
}

func fingerprintJSON(certPEM []byte, notAfter time.Time) []byte {
	return []byte(`{"sha256":"` + sha256hex(certPEM) + `","notAfter":"` + notAfter.UTC().Format(time.RFC3339) +
		`","serial":"42","generated":"` + time.Now().UTC().Format(time.RFC3339) + `"}`)
}

var tainerDomains = []string{"*.tainer.me", "tainer.me"}

func TestFetchFingerprint(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour).Truncate(time.Second)
	certPEM, keyPEM := makeCertPair(t, tainerDomains, notAfter)
	base := serveFiles(t, fingerprintJSON(certPEM, notAfter), certPEM, keyPEM)

	fp, err := FetchFingerprint(context.Background(), base)
	if err != nil {
		t.Fatalf("FetchFingerprint: %v", err)
	}
	if fp.SHA256 != sha256hex(certPEM) {
		t.Errorf("SHA256 = %q, want %q", fp.SHA256, sha256hex(certPEM))
	}
	if !fp.NotAfter.Equal(notAfter) {
		t.Errorf("NotAfter = %v, want %v", fp.NotAfter, notAfter)
	}
	if fp.Serial != "42" {
		t.Errorf("Serial = %q, want 42", fp.Serial)
	}
	if fp.Generated.IsZero() {
		t.Error("Generated is zero")
	}
}

func TestFetchFingerprintMalformed(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour)
	certPEM, keyPEM := makeCertPair(t, tainerDomains, notAfter)
	for name, body := range map[string]string{
		"not json":     "<html>enhance 404</html>",
		"missing sha":  `{"notAfter":"2027-01-01T00:00:00Z","serial":"1","generated":"2026-10-30T00:00:00Z"}`,
		"bad notAfter": `{"sha256":"abc123","notAfter":"next tuesday","serial":"1","generated":"2026-10-30T00:00:00Z"}`,
		"empty body":   "",
	} {
		base := serveFiles(t, []byte(body), certPEM, keyPEM)
		if _, err := FetchFingerprint(context.Background(), base); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

func TestFetchAndValidateHappyPath(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour).Truncate(time.Second)
	certPEM, keyPEM := makeCertPair(t, tainerDomains, notAfter)
	base := serveFiles(t, fingerprintJSON(certPEM, notAfter), certPEM, keyPEM)

	fp, err := FetchFingerprint(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	gotCert, gotKey, err := FetchAndValidate(context.Background(), base, fp)
	if err != nil {
		t.Fatalf("FetchAndValidate: %v", err)
	}
	if string(gotCert) != string(certPEM) || string(gotKey) != string(keyPEM) {
		t.Error("returned PEMs differ from served bytes")
	}
}

func TestFetchAndValidateEmptyBody(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour).Truncate(time.Second)
	certPEM, _ := makeCertPair(t, tainerDomains, notAfter)
	base := serveFiles(t, fingerprintJSON(certPEM, notAfter), nil, nil) // 0-byte files, as the live endpoint served 2026-10-30

	fp := Fingerprint{SHA256: sha256hex(certPEM), NotAfter: notAfter}
	if _, _, err := FetchAndValidate(context.Background(), base, fp); err == nil {
		t.Fatal("want error for empty bodies, got nil")
	}
}

func TestFetchAndValidateWrongDomains(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour).Truncate(time.Second)
	certPEM, keyPEM := makeCertPair(t, []string{"*.example.com", "example.com"}, notAfter)
	base := serveFiles(t, fingerprintJSON(certPEM, notAfter), certPEM, keyPEM)

	fp := Fingerprint{SHA256: sha256hex(certPEM), NotAfter: notAfter}
	if _, _, err := FetchAndValidate(context.Background(), base, fp); err == nil {
		t.Fatal("want error for wrong domains, got nil")
	}
}

func TestFetchAndValidateExpired(t *testing.T) {
	notAfter := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	certPEM, keyPEM := makeCertPair(t, tainerDomains, notAfter)
	base := serveFiles(t, fingerprintJSON(certPEM, notAfter), certPEM, keyPEM)

	fp := Fingerprint{SHA256: sha256hex(certPEM), NotAfter: notAfter}
	if _, _, err := FetchAndValidate(context.Background(), base, fp); err == nil {
		t.Fatal("want error for expired cert, got nil")
	}
}

func TestFetchAndValidateKeyMismatch(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour).Truncate(time.Second)
	certPEM, _ := makeCertPair(t, tainerDomains, notAfter)
	_, otherKey := makeCertPair(t, tainerDomains, notAfter) // key from a different pair
	base := serveFiles(t, fingerprintJSON(certPEM, notAfter), certPEM, otherKey)

	fp := Fingerprint{SHA256: sha256hex(certPEM), NotAfter: notAfter}
	if _, _, err := FetchAndValidate(context.Background(), base, fp); err == nil {
		t.Fatal("want error for key/cert mismatch, got nil")
	}
}

func TestFetchAndValidateHashMismatch(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour).Truncate(time.Second)
	certPEM, keyPEM := makeCertPair(t, tainerDomains, notAfter)
	base := serveFiles(t, fingerprintJSON(certPEM, notAfter), certPEM, keyPEM)

	fp := Fingerprint{SHA256: "deadbeef", NotAfter: notAfter}
	if _, _, err := FetchAndValidate(context.Background(), base, fp); err == nil {
		t.Fatal("want error for hash mismatch, got nil")
	}
}

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := sha256hex([]byte("hello")); got != want {
		t.Errorf("FileSHA256 = %q, want %q", got, want)
	}
	if _, err := FileSHA256(filepath.Join(dir, "missing")); err == nil {
		t.Error("want error for missing file, got nil")
	}
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Sys().(*syscall.Stat_t).Ino
}

func TestInstallInPlace(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tainer.me.crt")
	keyPath := filepath.Join(dir, "tainer.me.key")
	if err := os.WriteFile(certPath, []byte("old cert"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("old key"), 0600); err != nil {
		t.Fatal(err)
	}
	certIno, keyIno := inode(t, certPath), inode(t, keyPath)

	notAfter := time.Now().Add(60 * 24 * time.Hour)
	certPEM, keyPEM := makeCertPair(t, tainerDomains, notAfter)
	if err := InstallInPlace(certPEM, keyPEM, certPath, keyPath); err != nil {
		t.Fatalf("InstallInPlace: %v", err)
	}

	if inode(t, certPath) != certIno || inode(t, keyPath) != keyIno {
		t.Error("install replaced the inode — must write in place (rename severs per-file bind mounts)")
	}
	gotCert, _ := os.ReadFile(certPath)
	gotKey, _ := os.ReadFile(keyPath)
	if string(gotCert) != string(certPEM) || string(gotKey) != string(keyPEM) {
		t.Error("installed bytes differ")
	}
	certFI, _ := os.Stat(certPath)
	keyFI, _ := os.Stat(keyPath)
	if certFI.Mode().Perm() != 0644 {
		t.Errorf("cert perms = %v, want 0644", certFI.Mode().Perm())
	}
	if keyFI.Mode().Perm() != 0600 {
		t.Errorf("key perms = %v, want 0600", keyFI.Mode().Perm())
	}
}
