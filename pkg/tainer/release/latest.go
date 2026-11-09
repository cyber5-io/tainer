// Package release reads the published latest.json (get.tainer.dev) with
// a daily cache. Every failure is "no news": nothing here returns an
// error or blocks a user command for longer than fetchTimeout.
// Spec: docs/superpowers/specs/2026-11-08-releases-and-updates-design.md.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
)

// DefaultBaseURL is where latest.json is published. Like the cert
// endpoint it must live outside tainer.me (local resolver hijack).
const DefaultBaseURL = "https://get.tainer.dev/"

const checkInterval = 24 * time.Hour

// Test hooks.
var (
	// fetchTimeout bounds one whole fetch, independently of the caller's ctx.
	fetchTimeout = 10 * time.Second
	httpClient   = &http.Client{}
	now          = time.Now
)

// Artifact describes one downloadable package.
type Artifact struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Notes     string `json:"notes"`
	MinTainer string `json:"minTainer,omitempty"`
}

// Latest is the parsed latest.json.
type Latest struct {
	Tainer  Artifact `json:"tainer"`
	Menu    Artifact `json:"menu"`
	Control struct {
		Tag string `json:"tag"`
	} `json:"control"`
}

type cacheFile struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    Latest    `json:"latest"`
}

// BaseURL returns the publish endpoint (with trailing slash), honoring
// TAINER_RELEASE_URL.
func BaseURL() string {
	u := os.Getenv("TAINER_RELEASE_URL")
	if u == "" {
		return DefaultBaseURL
	}
	if !strings.HasSuffix(u, "/") {
		u += "/"
	}
	return u
}

// Check returns the latest release info. A cache younger than 24h is
// used unless force is set; otherwise latest.json is fetched (10s budget).
// On any failure the last cached value is returned; ok is false only
// when there is nothing at all.
func Check(ctx context.Context, force bool) (Latest, bool) {
	cached, haveCache := readCache()
	if haveCache && !force && now().Sub(cached.CheckedAt) < checkInterval {
		return cached.Latest, true
	}
	fresh, err := fetchLatest(ctx)
	if err != nil {
		if haveCache {
			return cached.Latest, true
		}
		return Latest{}, false
	}
	writeCache(cacheFile{CheckedAt: now(), Latest: fresh})
	return fresh, true
}

func fetchLatest(ctx context.Context) (Latest, error) {
	// The budget is our own, not the caller's command deadline.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	url := BaseURL() + "latest.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Latest{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Latest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Latest{}, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Latest{}, err
	}
	var l Latest
	if err := json.Unmarshal(body, &l); err != nil {
		return Latest{}, fmt.Errorf("parsing latest.json: %w", err)
	}
	if l.Tainer.Version == "" {
		return Latest{}, fmt.Errorf("latest.json: missing tainer.version")
	}
	return l, nil
}

func readCache() (cacheFile, bool) {
	data, err := os.ReadFile(config.ReleaseCacheFile())
	if err != nil {
		return cacheFile{}, false
	}
	var c cacheFile
	if err := json.Unmarshal(data, &c); err != nil || c.Latest.Tainer.Version == "" {
		return cacheFile{}, false
	}
	return c, true
}

func writeCache(c cacheFile) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	path := config.ReleaseCacheFile()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, data, 0644)
}
