package tls

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const renewalThresholdDays = 30

// CertExists checks if a certificate file exists.
func CertExists(certPath string) bool {
	_, err := os.Stat(certPath)
	return err == nil
}

// CheckExpiry reads a PEM certificate and returns its expiry date and whether it needs renewal.
func CheckExpiry(certPath string) (time.Time, bool, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reading cert: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, false, fmt.Errorf("no PEM block found in cert")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parsing cert: %w", err)
	}
	threshold := time.Now().Add(renewalThresholdDays * 24 * time.Hour)
	needsRenewal := cert.NotAfter.Before(threshold)
	return cert.NotAfter, needsRenewal, nil
}

// DownloadCert downloads cert+key from a URL and writes them to disk.
// Used for auto-renewal from GitHub Releases.
func DownloadCert(certURL, certPath, keyURL, keyPath string) error {
	if err := downloadFile(certURL, certPath); err != nil {
		return fmt.Errorf("downloading cert: %w", err)
	}
	if err := downloadFile(keyURL, keyPath); err != nil {
		return fmt.Errorf("downloading key: %w", err)
	}
	return nil
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0644)
}
