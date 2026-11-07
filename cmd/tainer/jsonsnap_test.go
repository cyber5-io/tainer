package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/doctor"
	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
	"github.com/cyber5-io/tainer/pkg/tainer/initcmd"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/networkcmd"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// The goldens in testdata/json were captured from the --json output as it was
// BEFORE the v1 envelope retrofit. Each snapshot test re-runs the emitter and
// requires that the only difference is the added top-level "v":1.

func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	func() {
		defer func() { os.Stdout = orig; _ = w.Close() }()
		fn()
	}()
	return <-done
}

// checkSnapshot compares got to a golden captured before the envelope existed.
// "v":1 is REQUIRED in got; it is removed before comparing. With wantV the
// golden itself already carries v and the documents must match exactly.
func checkSnapshot(got, want []byte, wantV bool) error {
	var gotDoc, wantDoc any
	if err := json.Unmarshal(got, &gotDoc); err != nil {
		return fmt.Errorf("output is not valid JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal(want, &wantDoc); err != nil {
		return fmt.Errorf("golden is not valid JSON: %v", err)
	}
	if !wantV {
		m, ok := gotDoc.(map[string]any)
		if !ok {
			return fmt.Errorf("output is not a JSON object: %s", got)
		}
		v, has := m["v"]
		if !has {
			return fmt.Errorf(`missing top-level "v": %s`, got)
		}
		if v != float64(1) {
			return fmt.Errorf(`"v" = %v, want 1`, v)
		}
		delete(m, "v")
	}
	if !reflect.DeepEqual(gotDoc, wantDoc) {
		return fmt.Errorf("output changed beyond adding v\n got: %s\nwant: %s", got, want)
	}
	return nil
}

func assertSnapshot(t *testing.T, name string, fn func()) {
	t.Helper()
	assertGolden(t, name, false, fn)
}

func assertGolden(t *testing.T, name string, wantV bool, fn func()) {
	t.Helper()
	got := captureStdout(t, fn)
	path := filepath.Join("testdata", "json", name+".golden")
	if os.Getenv("TAINER_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if err := checkSnapshot(got, want, wantV); err != nil {
		t.Errorf("%s: %v", name, err)
	}
}

func TestCheckSnapshotRequiresV(t *testing.T) {
	golden := []byte(`{"a":1}`)
	if err := checkSnapshot([]byte(`{"a":1}`), golden, false); err == nil {
		t.Error("a document without v must fail the snapshot")
	}
	if err := checkSnapshot([]byte(`{"v":2,"a":1}`), golden, false); err == nil {
		t.Error("v != 1 must fail")
	}
	if err := checkSnapshot([]byte(`{"v":1,"a":1}`), golden, false); err != nil {
		t.Errorf("valid doc rejected: %v", err)
	}
	if err := checkSnapshot([]byte(`{"v":1,"a":2}`), golden, false); err == nil {
		t.Error("changed key must fail")
	}
}

func TestJSONSnapshotStatusProject(t *testing.T) {
	snap := projectStatusSnapshot{
		Daemon:  daemonStatus{Socket: "/run/cyberstack.sock", Running: true, PID: 42, Reachable: true},
		Project: "blog", PodID: 7, State: "running", Domain: "blog.tainer.me",
		Ports:      []manifest.PortEntry{{Role: "web", Container: 80}},
		Containers: []containerStatus{{Role: "app", Name: "blog-app", State: "running", IP: "10.0.0.2"}},
	}
	assertSnapshot(t, "status-project", func() { runStatusProjectJSON(snap) })
	assertSnapshot(t, "status-project-empty", func() {
		runStatusProjectJSON(projectStatusSnapshot{Project: "gone", QueryError: "agent unreachable"})
	})
}

func TestJSONSnapshotStatusAll(t *testing.T) {
	snap := statusSnapshot{
		Daemon: daemonStatus{Socket: "/run/cyberstack.sock", Running: true, PID: 42, Reachable: true},
		Pods: []podStatus{
			{Name: "blog", PodID: 7, State: "running", Domain: "blog.tainer.me", Ports: []manifest.PortEntry{{Role: "web", Container: 80}}},
			{Name: "shop", PodID: 8, State: "stopped"},
		},
	}
	assertSnapshot(t, "status-all", func() { runStatusJSON(snap) })
	assertSnapshot(t, "status-all-empty", func() { runStatusJSON(statusSnapshot{}) })
}

func TestJSONSnapshotDoctor(t *testing.T) {
	rep := doctor.Report{
		Results: []doctor.Result{
			{Name: "daemon", Status: doctor.StatusOK, Detail: "reachable"},
			{Name: "pods", Status: doctor.StatusFail, Detail: "blog down", Hint: "tainer restart blog", Fixed: true},
		},
		StoppedPods: 2,
	}
	assertSnapshot(t, "doctor", func() { emitDoctorJSON(rep, false) })
}

func TestJSONSnapshotInit(t *testing.T) {
	steps := []initcmd.Step{{Action: "created", Kind: "file", Path: "tainer.yaml"}}
	assertSnapshot(t, "init", func() { emitInitJSON("blog", "/p/blog/tainer.yaml", steps) })
}

func TestJSONSnapshotStart(t *testing.T) {
	assertSnapshot(t, "start", func() {
		emitStartJSON(&pod.StartResult{
			Pod: "blog", PodID: 7, Domain: "blog.tainer.me",
			HTTPServices: []pod.HTTPSvcResult{{Role: "web", URL: "https://blog.tainer.me"}},
			TCPServices:  []pod.TCPSvcResult{{Role: "db", Host: "127.0.0.1:70"}},
		})
	})
	assertSnapshot(t, "start-nil-services", func() {
		emitStartJSON(&pod.StartResult{Pod: "blog", PodID: 7, AlreadyRunning: true})
	})
}

func TestJSONSnapshotStopDestroy(t *testing.T) {
	assertSnapshot(t, "stop", func() { emitStopJSON("blog", 2310*time.Millisecond) })
	assertSnapshot(t, "destroy", func() { emitDestroyJSON("blog", pod.DestroyClean, true, 1500*time.Millisecond) })
	assertSnapshot(t, "destroy-cancelled", func() { runDestroyCancelled("blog", tui.ModeJSON) })
}

func TestJSONSnapshotDB(t *testing.T) {
	assertSnapshot(t, "db-file", func() { emitDBFileJSON("blog", "/tmp/blog.sql", 2048, 900*time.Millisecond) })
}

func TestJSONSnapshotUpdate(t *testing.T) {
	assertSnapshot(t, "update-base-ok", func() { emitUpdateBaseJSON("wordpress", nil, time.Second) })
	assertSnapshot(t, "update-single-ok", func() { emitUpdateSingleJSON("blog", true, "", nil, time.Second) })
	assertSnapshot(t, "update-single-stopped", func() { emitUpdateSingleJSON("blog", false, "", nil, time.Second) })
	assertSnapshot(t, "update-all", func() {
		emitUpdateAllJSON([]updateAllPodResult{
			{Name: "blog", Updated: true, Started: true, ElapsedMs: 100},
			{Name: "docs", Updated: true, ElapsedMs: 50},
			{Name: "shop", Error: "manifest missing: x", ElapsedMs: 0},
		}, 3*time.Second)
	})
	assertSnapshot(t, "update-all-empty", func() { emitUpdateAllJSON(nil, 0) })
}

func TestJSONSnapshotNetwork(t *testing.T) {
	assertSnapshot(t, "network-show", func() { emitNetworkShowJSON("perf", "Performance") })
	assertSnapshot(t, "network-set-ok", func() {
		emitNetworkSetJSON(&networkcmd.SwitchResult{Previous: "compat", New: "perf", Restarted: []string{"blog"}, Failed: []string{"shop"}}, nil, 4*time.Second)
	})
	assertSnapshot(t, "network-set-nochange", func() {
		emitNetworkSetJSON(&networkcmd.SwitchResult{Previous: "perf", New: "perf", NoChange: true}, nil, time.Second)
	})
}

func sampleListRows() []listRow {
	return []listRow{
		{Name: "blog", Type: "wordpress", Domain: "blog.tainer.me", Path: "/p/blog", Status: "running"},
		{Name: "shop", Type: "nodejs", Domain: "shop.tainer.me", Path: "/p/shop", Status: "stopped"},
	}
}

// The list document was a bare array before the envelope; v cannot be added to
// an array, so it is the one family whose shape deliberately changes. The new
// shape has its own committed golden (see also TestJSONListEnvelope, which pins
// the rows against the pre-envelope array in list.golden).
func TestJSONSnapshotList(t *testing.T) {
	assertGolden(t, "list-v1", true, func() { emitListJSON(sampleListRows()) })
	assertGolden(t, "list-v1-empty", true, func() { emitListJSON(nil) })
}

func TestJSONSnapshotUpdateCheck(t *testing.T) {
	assertSnapshot(t, "update-check", func() {
		jsonDoc(imagecheck.Report{
			CheckedAt: "2026-11-02T12:00:00.000Z",
			Pods: []imagecheck.PodReport{
				{Name: "blog", Type: "wordpress", Domain: "blog.tainer.me", State: "running", UpdateAvailable: true,
					Parts: "WordPress · PHP 8.3 · MariaDB 11 · Caddy",
					Roles: []imagecheck.RoleReport{
						{Role: "web", Image: "ghcr.io/cyber5-io/tainer-caddy-web:2-alpine"},
						{Role: "app", Image: "ghcr.io/cyber5-io/tainer-wordpress-app:php-8.3", UpdateAvailable: true},
					}},
				{Name: "shop", Type: "react", Domain: "shop.tainer.me", State: "stopped",
					Parts: "React · Node 22 · Caddy", Roles: []imagecheck.RoleReport{}, Error: "registry unreachable"},
			},
			Available: 1,
			Unknown:   1,
		})
	})
}
