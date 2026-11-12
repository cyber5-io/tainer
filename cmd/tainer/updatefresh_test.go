package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/release"
)

// tain-134: an update the user asked for reads latest.json fresh, not the
// daily cache; a fetch that fails falls back to the cache as before.

// realRelease swaps the fake release check for the real one, pointed at a
// local server that serves tainer and the menu at version serverVersion (or
// fails with status). cachedVersion, when set, is written as a cache from a
// minute ago — young enough that a cached read would use it.
func realRelease(t *testing.T, serverVersion string, status int, cachedVersion string) *int32 {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		fmt.Fprintf(w, `{"tainer":{"version":%q,"url":"https://get.tainer.dev/t.pkg","sha256":"abc","notes":"fresh"},"menu":{"version":%q,"url":"https://get.tainer.dev/m.pkg","sha256":"def","notes":""},"control":{"tag":"x"}}`,
			serverVersion, serverVersion)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TAINER_RELEASE_URL", srv.URL)
	if cachedVersion != "" {
		var cache struct {
			CheckedAt time.Time      `json:"checkedAt"`
			Latest    release.Latest `json:"latest"`
		}
		cache.CheckedAt = time.Now().Add(-time.Minute)
		cache.Latest.Tainer = release.Artifact{Version: cachedVersion, URL: "https://get.tainer.dev/t.pkg", SHA256: "abc"}
		cache.Latest.Menu = release.Artifact{Version: cachedVersion, URL: "https://get.tainer.dev/m.pkg", SHA256: "def"}
		data, _ := json.Marshal(cache)
		if err := os.MkdirAll(filepath.Dir(config.ReleaseCacheFile()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.ReleaseCacheFile(), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	releaseCheck = release.Check
	return &hits
}

func TestUpdateListingFetchesFreshOverTheDailyCache(t *testing.T) {
	selfTestHome(t)
	f := newFakeSelf()
	f.tainer, f.menu, f.stdin = "1.5.6", "1.5.6", "n\n"
	f.install(t)
	hits := realRelease(t, "1.6.0", http.StatusOK, "1.5.7")
	out := string(runUpdateCmd(t, nil))
	if !strings.Contains(out, "tainer 1.5.6 → 1.6.0") || strings.Contains(out, "1.5.7") {
		t.Errorf("the listing must offer what the server has now, not the cache:\n%s", out)
	}
	if *hits != 1 {
		t.Errorf("latest.json fetched %d times, want 1", *hits)
	}
}

func TestUpdateInAProjectHintsFromAFreshCheck(t *testing.T) {
	selfTestHome(t)
	blog := registerProject(t, t.TempDir(), "blog")
	t.Chdir(blog)
	f := newFakeSelf()
	f.tainer, f.menu, f.stdin = "1.5.6", "1.5.6", "n\n"
	f.install(t)
	// The cache says tainer is up to date; the server knows better.
	realRelease(t, "1.6.0", http.StatusOK, "1.5.6")
	if out := string(runUpdateCmd(t, nil)); !strings.Contains(out, tainerHintHere) {
		t.Errorf("missing the tainer hint from a fresh check:\n%s", out)
	}
}

func TestUpdateCoreWithoutInstallerReportsTheFreshVersion(t *testing.T) {
	selfTestHome(t)
	orig := selfUpdateSupported
	selfUpdateSupported = false
	t.Cleanup(func() { selfUpdateSupported = orig })
	f := newFakeSelf()
	f.tainer, f.menu = "1.5.6", "1.5.6"
	f.install(t)
	realRelease(t, "1.6.0", http.StatusOK, "1.5.7")
	if out := string(runUpdateCmd(t, []string{"core"})); !strings.Contains(out, "tainer 1.6.0 available") {
		t.Errorf("update core must report the fresh version:\n%s", out)
	}
}

func TestUpdateCoreInstallerChecksFresh(t *testing.T) {
	// On macOS the installer's own check (no listing shown) always fetched fresh.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	hits := realRelease(t, "1.6.0", http.StatusOK, "1.5.7")
	ops := liveInstallOps(true, nil, nil)
	latest, ok := ops.check(context.Background())
	if !ok || latest.Tainer.Version != "1.6.0" || *hits != 1 {
		t.Errorf("installer check: %+v ok=%v hits=%d", latest.Tainer, ok, *hits)
	}
}

func TestAFailedFreshFetchFallsBackToTheCache(t *testing.T) {
	selfTestHome(t)
	f := newFakeSelf()
	f.tainer, f.menu, f.stdin = "1.5.6", "1.5.6", "n\n"
	f.install(t)
	realRelease(t, "", http.StatusBadGateway, "1.5.7")
	if out := string(runUpdateCmd(t, nil)); !strings.Contains(out, "tainer 1.5.6 → 1.5.7") {
		t.Errorf("offline, the cached release still counts:\n%s", out)
	}
}

func TestAFailedFetchWithNoCacheIsNoNewsAndBlocksNothing(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "blog")
	f := newFakeSelf()
	f.tainer, f.menu = "1.5.6", "1.5.6"
	f.install(t)
	realRelease(t, "", http.StatusBadGateway, "")
	out := string(runUpdateCmd(t, []string{"--yes"}))
	if strings.Contains(out, "tainer 1.5.6 →") {
		t.Errorf("no release info means no tainer offer:\n%s", out)
	}
	if got := podLog(f.log); len(got) != 1 || got[0] != "pod:blog" {
		t.Errorf("pods still update when the release can't be fetched, ran %v\n%s", f.log, out)
	}
}

func TestPassiveReadsStayCached(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	hits := realRelease(t, "1.6.0", http.StatusOK, "1.5.7")
	latest, ok := release.Check(context.Background(), false)
	if !ok || latest.Tainer.Version != "1.5.7" || *hits != 0 {
		t.Errorf("a cached read within 24h must not fetch: %+v ok=%v hits=%d", latest.Tainer, ok, *hits)
	}
}
