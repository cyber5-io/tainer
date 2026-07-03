package pod

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// WaitHTTPReady polls https://domain until the site answers with a
// non-5xx status, the timeout lapses, or ctx is cancelled. It closes
// the gap between "containers are running" and "the browser works":
// right after start, caddy may still be loading the updated config
// and php-fpm/node is still warming — a user who clicks the printed
// URL the moment the Ready line appears would hit a 502 that clears
// itself seconds later.
//
// The dial is pinned to 127.0.0.1:443 with SNI set to domain, so the
// probe exercises the real router path without depending on DNS.
// Certificate verification is skipped — this is a reachability probe,
// not a trust decision.
//
// Returns (lastStatusCode, true) once the site answers below 500,
// or (last observed status or 0, false) on timeout/cancel. Callers
// should treat false as "still warming up", not as a start failure —
// the pod itself started fine.
func WaitHTTPReady(ctx context.Context, domain string, timeout time.Duration) (int, bool) {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(dctx context.Context, netw, _ string) (net.Conn, error) {
				return dialer.DialContext(dctx, netw, "127.0.0.1:443")
			},
			TLSClientConfig:   &tls.Config{ServerName: domain, InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
	}

	deadline := time.Now().Add(timeout)
	lastCode := 0
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return lastCode, false
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
		if err != nil {
			return lastCode, false
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			lastCode = resp.StatusCode
			if resp.StatusCode < 500 {
				return resp.StatusCode, true
			}
		}
		select {
		case <-ctx.Done():
			return lastCode, false
		case <-time.After(500 * time.Millisecond):
		}
	}
}
