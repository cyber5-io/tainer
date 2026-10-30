package tls

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Fingerprint describes the cert currently published at the renewal
// endpoint (certs.tainer.dev). See the 2026-10-30 cert auto-renew spec.
type Fingerprint struct {
	SHA256    string    `json:"sha256"`
	NotAfter  time.Time `json:"notAfter"`
	Serial    string    `json:"serial"`
	Generated time.Time `json:"generated"`
}

var refreshClient = &http.Client{Timeout: 5 * time.Second}

// FetchFingerprint GETs <baseURL>fingerprint.json and parses it.
func FetchFingerprint(ctx context.Context, baseURL string) (Fingerprint, error) {
	body, err := fetch(ctx, baseURL+"fingerprint.json")
	if err != nil {
		return Fingerprint{}, err
	}
	var fp Fingerprint
	if err := json.Unmarshal(body, &fp); err != nil {
		return Fingerprint{}, fmt.Errorf("parsing fingerprint.json: %w", err)
	}
	if fp.SHA256 == "" {
		return Fingerprint{}, fmt.Errorf("fingerprint.json: missing sha256")
	}
	if fp.NotAfter.IsZero() {
		return Fingerprint{}, fmt.Errorf("fingerprint.json: missing notAfter")
	}
	return fp, nil
}

// FileSHA256 returns the lowercase hex sha256 of the file's bytes.
func FileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// FetchAndValidate downloads fullchain.pem and privkey.pem from baseURL
// and validates them against fp before anything touches the live files:
// non-empty, parseable, covering *.tainer.me + tainer.me, unexpired,
// hash and expiry matching fp, and the key pairing with the leaf cert.
func FetchAndValidate(ctx context.Context, baseURL string, fp Fingerprint) (certPEM, keyPEM []byte, err error) {
	certPEM, err = fetch(ctx, baseURL+"fullchain.pem")
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err = fetch(ctx, baseURL+"privkey.pem")
	if err != nil {
		return nil, nil, err
	}
	if len(bytes.TrimSpace(certPEM)) == 0 || len(bytes.TrimSpace(keyPEM)) == 0 {
		return nil, nil, fmt.Errorf("endpoint served empty cert or key")
	}
	if got := hexSHA256(certPEM); got != fp.SHA256 {
		return nil, nil, fmt.Errorf("fullchain.pem sha256 %s does not match fingerprint %s", got, fp.SHA256)
	}
	leaf, err := parseLeaf(certPEM)
	if err != nil {
		return nil, nil, err
	}
	if err := checkDomains(leaf); err != nil {
		return nil, nil, err
	}
	if !leaf.NotAfter.After(time.Now()) {
		return nil, nil, fmt.Errorf("downloaded cert already expired %s", leaf.NotAfter.Format("2006-01-02"))
	}
	if leaf.NotAfter.Unix() != fp.NotAfter.Unix() {
		return nil, nil, fmt.Errorf("cert notAfter %s does not match fingerprint %s", leaf.NotAfter, fp.NotAfter)
	}
	if err := checkKeyPairs(leaf, keyPEM); err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

// InstallInPlace writes key then cert over the existing files IN PLACE
// (os.WriteFile, never rename): the router bind-mounts each file
// individually, and renaming over the source permanently severs the
// mount inside the container (verified 2026-10-30).
func InstallInPlace(certPEM, keyPEM []byte, certPath, keyPath string) error {
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("writing key: %w", err)
	}
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return fmt.Errorf("writing cert: %w", err)
	}
	// os.WriteFile only applies perms on create; enforce on existing files.
	if err := os.Chmod(keyPath, 0600); err != nil {
		return fmt.Errorf("key perms: %w", err)
	}
	if err := os.Chmod(certPath, 0644); err != nil {
		return fmt.Errorf("cert perms: %w", err)
	}
	return nil
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := refreshClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func hexSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func parseLeaf(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("fullchain.pem: no certificate PEM block")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing leaf certificate: %w", err)
	}
	return leaf, nil
}

func checkDomains(leaf *x509.Certificate) error {
	for _, want := range []string{"*.tainer.me", "tainer.me"} {
		found := false
		for _, name := range leaf.DNSNames {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("certificate does not cover %s (has %v)", want, leaf.DNSNames)
		}
	}
	return nil
}

// checkKeyPairs verifies keyPEM's public key matches the leaf's.
func checkKeyPairs(leaf *x509.Certificate, keyPEM []byte) error {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return fmt.Errorf("privkey.pem: no PEM block")
	}
	var key any
	var err error
	switch block.Type {
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	}
	if err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return fmt.Errorf("unsupported private key type %T", key)
	}
	pub, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return fmt.Errorf("encoding key's public half: %w", err)
	}
	leafPub, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		return fmt.Errorf("encoding cert's public key: %w", err)
	}
	if !bytes.Equal(pub, leafPub) {
		return fmt.Errorf("private key does not match certificate")
	}
	return nil
}
