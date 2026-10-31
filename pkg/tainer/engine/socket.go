package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// DefaultSocketPath is where cyberstackd listens by default. On macOS it
// mirrors cmd/cyberstackd/main.go's defaultPath("cyberstack.sock"); on Linux
// the daemon is systemd socket-activated at a fixed system path.
func DefaultSocketPath() string {
	home, _ := os.UserHomeDir()
	return socketPathFor(goruntime.GOOS, home)
}

func socketPathFor(goos, home string) string {
	if goos == "linux" {
		return "/run/cyberstack/cyberstack.sock"
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cyberstack", "cyberstack.sock")
}

// PostJSON POSTs body as application/json to path on the daemon's Unix
// socket (honouring a unix:// DOCKER_HOST, like New). It reports success
// for any 2xx and an error otherwise; callers decide whether to care.
func PostJSON(ctx context.Context, path string, body []byte) error {
	socket := DefaultSocketPath()
	if env := os.Getenv("DOCKER_HOST"); strings.HasPrefix(env, "unix://") {
		socket = strings.TrimPrefix(env, "unix://")
	}
	hc := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		DisableKeepAlives: true,
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://cyberstack"+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("engine: post %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("engine: post %s: %w", path, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("engine: post %s: unexpected status %s", path, resp.Status)
	}
	return nil
}
