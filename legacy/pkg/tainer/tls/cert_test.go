package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createTestCert(t *testing.T, dir string, expiry time.Time) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "*.tainer.me"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     expiry,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	os.WriteFile(filepath.Join(dir, "tainer.me.crt"), certPEM, 0644)
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	os.WriteFile(filepath.Join(dir, "tainer.me.key"), keyPEM, 0644)
}

func TestCheckExpiry_Valid(t *testing.T) {
	dir := t.TempDir()
	createTestCert(t, dir, time.Now().Add(365*24*time.Hour))
	certPath := filepath.Join(dir, "tainer.me.crt")

	expiry, needsRenewal, err := CheckExpiry(certPath)
	if err != nil {
		t.Fatalf("CheckExpiry() error: %v", err)
	}
	if needsRenewal {
		t.Error("cert expiring in 365 days should not need renewal")
	}
	if expiry.Before(time.Now()) {
		t.Error("expiry should be in the future")
	}
}

func TestCheckExpiry_NeedsRenewal(t *testing.T) {
	dir := t.TempDir()
	createTestCert(t, dir, time.Now().Add(15*24*time.Hour)) // 15 days
	certPath := filepath.Join(dir, "tainer.me.crt")

	_, needsRenewal, err := CheckExpiry(certPath)
	if err != nil {
		t.Fatalf("CheckExpiry() error: %v", err)
	}
	if !needsRenewal {
		t.Error("cert expiring in 15 days should need renewal")
	}
}

func TestCheckExpiry_NoCert(t *testing.T) {
	_, _, err := CheckExpiry("/nonexistent/cert.crt")
	if err == nil {
		t.Error("CheckExpiry() should error on missing cert")
	}
}

func TestCertExists(t *testing.T) {
	dir := t.TempDir()
	if CertExists(filepath.Join(dir, "tainer.me.crt")) {
		t.Error("CertExists() should be false for missing cert")
	}
	createTestCert(t, dir, time.Now().Add(365*24*time.Hour))
	if !CertExists(filepath.Join(dir, "tainer.me.crt")) {
		t.Error("CertExists() should be true for existing cert")
	}
}
