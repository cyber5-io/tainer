// Package certsync keeps the local *.tainer.me certificate fresh by
// checking the Cyber5 renewal endpoint at most once a day, on the first
// engine-touching command (no daemon, no cron). All failures are
// warnings: a cert refresh must never block or fail a user command.
// Spec: docs/superpowers/specs/2026-10-30-cert-auto-renew-design.md.
package certsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/tls"
)

// DefaultBaseURL is where the Cyber5 renewer publishes the wildcard
// cert. Override with TAINER_CERT_URL for testing.
const DefaultBaseURL = "https://certs.tainer.me/tainer.me/"

const checkInterval = 24 * time.Hour

// BaseURL returns the publish endpoint, honoring TAINER_CERT_URL.
func BaseURL() string {
	if u := os.Getenv("TAINER_CERT_URL"); u != "" {
		return u
	}
	return DefaultBaseURL
}

// Outcome records the last refresh attempt for the daily gate and for
// `tainer doctor`.
type Outcome struct {
	CheckedAt time.Time `json:"checkedAt"`
	Refreshed bool      `json:"refreshed"`
	Err       string    `json:"err,omitempty"`
}

func stampPath() string { return filepath.Join(config.CertsDir(), ".last-check") }

// ReadLastOutcome returns the last recorded attempt, if any.
func ReadLastOutcome() (Outcome, bool) {
	data, err := os.ReadFile(stampPath())
	if err != nil {
		return Outcome{}, false
	}
	var out Outcome
	if err := json.Unmarshal(data, &out); err != nil {
		return Outcome{}, false
	}
	return out, true
}

func writeOutcome(out Outcome) {
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	_ = os.MkdirAll(config.CertsDir(), 0755)
	_ = os.WriteFile(stampPath(), data, 0644)
}

// shouldAttempt gates the daily check: at most one attempt per
// checkInterval, and only when the cert is missing or inside the
// renewal window. The outside-window path does not advance the stamp —
// it costs one local expiry parse, never network.
func shouldAttempt(now time.Time) bool {
	if out, ok := ReadLastOutcome(); ok && now.Sub(out.CheckedAt) < checkInterval {
		return false
	}
	certPath := config.CertFile()
	if !tls.CertExists(certPath) {
		return true
	}
	_, needsRenewal, err := tls.CheckExpiry(certPath)
	if err != nil {
		return true // unreadable cert is as bad as a missing one
	}
	return needsRenewal
}

// DailyRefresh runs the whole check-and-fetch flow. reload is called
// after a successful install (the caller passes router.ReloadCaddy
// bound to its engine client). Never returns an error and never
// panics: cert hygiene must not take down the user's command.
func DailyRefresh(ctx context.Context, reload func(context.Context) error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "warning: cert refresh: %v\n", r)
		}
	}()

	now := time.Now()
	if !shouldAttempt(now) {
		return
	}

	fail := func(err error) {
		writeOutcome(Outcome{CheckedAt: now, Err: err.Error()})
		fmt.Fprintf(os.Stderr, "warning: tainer.me certificate refresh failed: %v (will retry tomorrow; see `tainer doctor`)\n", err)
	}

	base := BaseURL()
	fp, err := tls.FetchFingerprint(ctx, base)
	if err != nil {
		fail(err)
		return
	}

	certPath, keyPath := config.CertFile(), config.KeyFile()
	if local, err := tls.FileSHA256(certPath); err == nil && local == fp.SHA256 {
		// Server has nothing newer (its renewal is still pending).
		writeOutcome(Outcome{CheckedAt: now})
		return
	}

	certPEM, keyPEM, err := tls.FetchAndValidate(ctx, base, fp)
	if err != nil {
		fail(err)
		return
	}
	if err := tls.InstallInPlace(certPEM, keyPEM, certPath, keyPath); err != nil {
		fail(err)
		return
	}
	if err := reload(ctx); err != nil {
		// Files are installed; caddy picks them up on its next start.
		fmt.Fprintf(os.Stderr, "warning: certificate installed but router reload failed: %v\n", err)
	}
	writeOutcome(Outcome{CheckedAt: now, Refreshed: true})
	fmt.Printf("tainer: refreshed tainer.me certificate (valid to %s)\n", fp.NotAfter.Format("2006-01-02"))
}
