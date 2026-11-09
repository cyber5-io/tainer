package release

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
)

const goodJSON = `{"tainer":{"version":"1.5.0","url":"https://get.tainer.dev/t.pkg","sha256":"abc","notes":"n","minTainer":"1.4.0"},"menu":{"version":"0.1.0","url":"https://get.tainer.dev/m.pkg","sha256":"def","notes":""},"control":{"tag":"1.5.0"},"extra":{"x":1}}`

func setup(t *testing.T, h http.HandlerFunc) *int32 {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TAINER_RELEASE_URL", srv.URL+"/")
	return &hits
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/latest.json" {
		http.NotFound(w, r)
		return
	}
	w.Write([]byte(goodJSON))
}

func TestCheckUsesCacheWithin24h(t *testing.T) {
	hits := setup(t, okHandler)
	l, ok := Check(context.Background(), false)
	if !ok || l.Tainer.Version != "1.5.0" || l.Control.Tag != "1.5.0" || l.Tainer.MinTainer != "1.4.0" {
		t.Fatalf("first: %+v ok=%v", l, ok)
	}
	if _, ok := Check(context.Background(), false); !ok {
		t.Fatal("second not ok")
	}
	if *hits != 1 {
		t.Fatalf("hits=%d want 1", *hits)
	}
	Check(context.Background(), true)
	if *hits != 2 {
		t.Fatalf("hits=%d want 2 after force", *hits)
	}
}

func TestCheckRefetchesWhenCacheStale(t *testing.T) {
	hits := setup(t, okHandler)
	Check(context.Background(), false)
	base := time.Now()
	old := now
	now = func() time.Time { return base.Add(25 * time.Hour) }
	t.Cleanup(func() { now = old })
	Check(context.Background(), false)
	if *hits != 2 {
		t.Fatalf("hits=%d want 2", *hits)
	}
}

func TestFetchTimeoutIsNoNews(t *testing.T) {
	done := make(chan struct{})
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
	})
	t.Cleanup(func() { close(done) })
	oldT := fetchTimeout
	fetchTimeout = 300 * time.Millisecond
	t.Cleanup(func() { fetchTimeout = oldT })

	// Caller context is already unbounded; the budget must bound the call.
	start := time.Now()
	_, ok := Check(context.Background(), false)
	if ok {
		t.Fatal("ok with no cache")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}

	// With a previous (stale) cache, it is returned.
	writeCache(cacheFile{CheckedAt: time.Now().Add(-48 * time.Hour), Latest: Latest{Tainer: Artifact{Version: "1.4.0"}}})
	l, ok := Check(context.Background(), false)
	if !ok || l.Tainer.Version != "1.4.0" {
		t.Fatalf("want stale cache, got %+v ok=%v", l, ok)
	}
}

func TestDefaultFetchBudgetIs10s(t *testing.T) {
	if fetchTimeout != 10*time.Second {
		t.Fatalf("fetchTimeout=%v", fetchTimeout)
	}
}

func TestCheckRejectsMissingFields(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tainer":{"url":"u"},"menu":{},"control":{}}`))
	})
	if _, ok := Check(context.Background(), false); ok {
		t.Fatal("ok for invalid json")
	}
	if _, err := os.Stat(config.ReleaseCacheFile()); err == nil {
		t.Fatal("cache written for invalid json")
	}
	// existing cache untouched and returned
	writeCache(cacheFile{CheckedAt: time.Now().Add(-48 * time.Hour), Latest: Latest{Tainer: Artifact{Version: "1.4.0"}}})
	before, _ := os.ReadFile(config.ReleaseCacheFile())
	l, ok := Check(context.Background(), true)
	after, _ := os.ReadFile(config.ReleaseCacheFile())
	if !ok || l.Tainer.Version != "1.4.0" || string(before) != string(after) {
		t.Fatalf("cache disturbed: %+v ok=%v", l, ok)
	}
}

func TestCheckFailureStatusIsNoNews(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) })
	if _, ok := Check(context.Background(), false); ok {
		t.Fatal("ok on 500")
	}
}

func TestCheckIgnoresUnknownKeys(t *testing.T) {
	setup(t, okHandler)
	l, ok := Check(context.Background(), false)
	if !ok || l.Menu.Version != "0.1.0" {
		t.Fatalf("%+v %v", l, ok)
	}
	var back cacheFile
	data, _ := os.ReadFile(config.ReleaseCacheFile())
	if err := json.Unmarshal(data, &back); err != nil || back.Latest.Tainer.SHA256 != "abc" {
		t.Fatalf("cache: %v %+v", err, back)
	}
}

func TestBaseURL(t *testing.T) {
	t.Setenv("TAINER_RELEASE_URL", "")
	if BaseURL() != "https://get.tainer.dev/" {
		t.Fatal(BaseURL())
	}
	t.Setenv("TAINER_RELEASE_URL", "http://x/")
	if BaseURL() != "http://x/" {
		t.Fatal(BaseURL())
	}
}
