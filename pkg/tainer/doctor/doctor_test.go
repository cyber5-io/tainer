package doctor

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/certsync"
	"github.com/cyber5-io/tainer/pkg/tainer/config"
)

func TestReportHealthy(t *testing.T) {
	cases := []struct {
		name    string
		results []Result
		want    bool
	}{
		{"empty", nil, true},
		{"all ok", []Result{{Status: StatusOK}, {Status: StatusOK}}, true},
		{"warns are healthy", []Result{{Status: StatusOK}, {Status: StatusWarn}}, true},
		{"fail is unhealthy", []Result{{Status: StatusOK}, {Status: StatusFail}}, false},
		{"skip is unhealthy", []Result{{Status: StatusOK}, {Status: StatusSkip}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Report{Results: c.results}
			if got := r.Healthy(); got != c.want {
				t.Errorf("Healthy() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestReportCounts(t *testing.T) {
	r := Report{Results: []Result{
		{Status: StatusOK}, {Status: StatusOK},
		{Status: StatusWarn},
		{Status: StatusFail},
		{Status: StatusSkip}, {Status: StatusSkip}, {Status: StatusSkip},
	}}
	ok, warn, fail, skip := r.Counts()
	if ok != 2 || warn != 1 || fail != 1 || skip != 3 {
		t.Errorf("Counts() = %d/%d/%d/%d, want 2/1/1/3", ok, warn, fail, skip)
	}
}

func writeExpiredCert(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(config.CertsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "*.tainer.me"},
		DNSNames:     []string{"*.tainer.me", "tainer.me"},
		NotBefore:    time.Now().Add(-100 * 24 * time.Hour),
		NotAfter:     time.Now().Add(-24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(config.CertFile(), certPEM, 0644); err != nil {
		t.Fatal(err)
	}
}

func tlsResult(t *testing.T, d *run) Result {
	t.Helper()
	for _, r := range d.report.Results {
		if r.Name == "tls" {
			return r
		}
	}
	t.Fatal("no tls result")
	return Result{}
}

// Expired cert + a recorded failed refresh attempt: the fail line must
// name the endpoint and the last error so the user can see WHY the
// auto-refresh isn't saving them.
func TestCheckTLSExpiredWithFailedRefresh(t *testing.T) {
	writeExpiredCert(t)
	out, _ := json.Marshal(certsync.Outcome{CheckedAt: time.Now(), Err: "connection refused"})
	if err := os.WriteFile(filepath.Join(config.CertsDir(), ".last-check"), out, 0644); err != nil {
		t.Fatal(err)
	}

	d := &run{}
	d.checkTLS()
	r := tlsResult(t, d)
	if r.Status != StatusFail {
		t.Errorf("status = %v, want fail", r.Status)
	}
	combined := r.Detail + " " + r.Hint
	if !strings.Contains(combined, certsync.BaseURL()) {
		t.Errorf("tls result does not name the refresh endpoint: %q", combined)
	}
	if !strings.Contains(combined, "connection refused") {
		t.Errorf("tls result does not surface the last refresh error: %q", combined)
	}
}

// Expired cert, no refresh attempt recorded yet: the hint must say the
// cert refreshes automatically on the next command, not "reinstall".
func TestCheckTLSExpiredNoAttemptYet(t *testing.T) {
	writeExpiredCert(t)

	d := &run{}
	d.checkTLS()
	r := tlsResult(t, d)
	if r.Status != StatusFail {
		t.Errorf("status = %v, want fail", r.Status)
	}
	if !strings.Contains(strings.ToLower(r.Detail+" "+r.Hint), "automatic") {
		t.Errorf("hint should mention the automatic refresh: %q", r.Hint)
	}
	if strings.Contains(strings.ToLower(r.Hint), "reinstall") || strings.Contains(strings.ToLower(r.Hint), "update tainer") {
		t.Errorf("stale manual hint survived: %q", r.Hint)
	}
}
