package imagecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

const wpYAML = `version: 2
project:
  name: %s
  type: wordpress
  domain: %s.tainer.me
runtime:
  php: "8.3"
  database: mariadb
`

const reactYAML = `version: 2
project:
  name: %s
  type: react
  domain: %s.tainer.me
runtime:
  node: "22"
  database: none
`

func mkProject(t *testing.T, name, tpl string) registry.Project {
	t.Helper()
	dir := t.TempDir()
	body := []byte(fmt.Sprintf(tpl, name, name))
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return registry.Project{Path: dir}
}

type fakes struct {
	mu     sync.Mutex
	remote map[string]int
	local  map[string]string // ref -> image id
	digest map[string]string // ref -> remote digest
	rerr   map[string]error
	lerr   map[string]error
	pods   []pod.Pod
}

func (f *fakes) deps() Deps {
	return Deps{
		EnginePods: func(ctx context.Context) ([]pod.Pod, error) { return f.pods, nil },
		ImageInspect: func(ctx context.Context, ref string) (engine.ImageInfo, error) {
			if err := f.lerr[ref]; err != nil {
				return engine.ImageInfo{}, err
			}
			return engine.ImageInfo{ID: f.local[ref], Architecture: "arm64"}, nil
		},
		RemoteConfigDigest: func(ctx context.Context, ref Ref, arch string) (string, error) {
			key := ref.Host + "/" + ref.Repo + ":" + ref.Tag
			f.mu.Lock()
			f.remote[key]++
			f.mu.Unlock()
			if err := f.rerr[key]; err != nil {
				return "", err
			}
			return f.digest[key], nil
		},
	}
}

func newFakes() *fakes {
	return &fakes{remote: map[string]int{}, local: map[string]string{}, digest: map[string]string{},
		rerr: map[string]error{}, lerr: map[string]error{}}
}

const (
	web = "ghcr.io/cyber5-io/tainer-caddy-web:2-alpine"
	wpA = "ghcr.io/cyber5-io/tainer-wordpress-app:php-8.3"
	db  = "ghcr.io/cyber5-io/tainer-mariadb-db:11"
	rA  = "ghcr.io/cyber5-io/tainer-react-app:node-22"
)

func sameEverywhere(f *fakes, refs ...string) {
	for _, r := range refs {
		f.local[r] = "sha256:" + r
		f.digest[r] = "sha256:" + r
	}
}

func TestRunDedupesAndClassifies(t *testing.T) {
	f := newFakes()
	sameEverywhere(f, web, db, rA)
	f.local[wpA] = "old"
	f.digest[wpA] = "sha256:new"
	f.pods = []pod.Pod{
		{Name: "blog", Containers: []pod.Container{{Role: "web", State: "running"}}},
		{Name: "app", Containers: []pod.Container{{Role: "web", State: "exited"}}},
	}
	projects := map[string]registry.Project{
		"blog":  mkProject(t, "blog", wpYAML),
		"app":   mkProject(t, "app", reactYAML),
		"newer": mkProject(t, "newer", reactYAML), // no engine pod: stopped, up to date
	}
	rep, err := Run(context.Background(), f.deps(), projects)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Pods) != 3 {
		t.Fatalf("pods=%d want 3 (registry-only included)", len(rep.Pods))
	}
	if rep.Available != 1 || rep.UpToDate != 2 || rep.Unknown != 0 {
		t.Fatalf("counts %+v", rep)
	}
	if n := f.remote[web]; n != 1 {
		t.Fatalf("caddy-web fetched %d times, want 1", n)
	}
	app, blog, newer := rep.Pods[0], rep.Pods[1], rep.Pods[2]
	if app.Name != "app" || blog.Name != "blog" || newer.Name != "newer" {
		t.Fatalf("not sorted: %s %s %s", app.Name, blog.Name, newer.Name)
	}
	if newer.State != "stopped" || newer.UpdateAvailable || newer.Error != "" || len(newer.Roles) != 0 || newer.Parts != "React · Node 22 · Caddy" {
		t.Fatalf("no-container project: %+v", newer)
	}
	if n := f.remote[rA]; n != 1 {
		t.Fatalf("react app fetched %d times, want 1 (no checks for container-less project)", n)
	}
	if !blog.UpdateAvailable || blog.State != "running" || app.State != "stopped" {
		t.Fatalf("blog/app: %+v / %+v", blog, app)
	}
	if blog.Parts != "WordPress · PHP 8.3 · MariaDB 11 · Caddy" {
		t.Fatalf("parts %q", blog.Parts)
	}
	for _, r := range blog.Roles {
		if r.Role == "app" != r.UpdateAvailable {
			t.Fatalf("role %+v", r)
		}
	}
	if len(blog.Roles) != 3 || blog.Roles[0].Image != web {
		t.Fatalf("roles %+v", blog.Roles)
	}
}

func TestRunRemoteFailureIsUnknown(t *testing.T) {
	f := newFakes()
	sameEverywhere(f, web, rA)
	f.rerr[rA] = errors.New("registry unreachable")
	f.pods = []pod.Pod{{Name: "app", Containers: []pod.Container{{Role: "web", State: "running"}}}}
	rep, err := Run(context.Background(), f.deps(), map[string]registry.Project{"app": mkProject(t, "app", reactYAML)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Unknown != 1 || rep.Available != 0 || rep.UpToDate != 0 {
		t.Fatalf("counts %+v", rep)
	}
	if p := rep.Pods[0]; p.UpdateAvailable || p.Error == "" {
		t.Fatalf("pod %+v", p)
	}
}

func TestRunLocalMissingIsUnknown(t *testing.T) {
	f := newFakes()
	sameEverywhere(f, web, rA)
	f.lerr[rA] = engine.ErrImageNotFound
	f.pods = []pod.Pod{{Name: "app", Containers: []pod.Container{{Role: "web", State: "running"}}}}
	rep, _ := Run(context.Background(), f.deps(), map[string]registry.Project{"app": mkProject(t, "app", reactYAML)})
	if rep.Unknown != 1 || rep.Pods[0].UpdateAvailable || rep.Pods[0].Error == "" {
		t.Fatalf("report %+v", rep)
	}
}

func TestRunEnginePodsError(t *testing.T) {
	f := newFakes()
	d := f.deps()
	d.EnginePods = func(ctx context.Context) ([]pod.Pod, error) { return nil, errors.New("down") }
	if _, err := Run(context.Background(), d, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestReportJSONShape(t *testing.T) {
	f := newFakes()
	sameEverywhere(f, web, rA)
	f.pods = []pod.Pod{{Name: "app", Containers: []pod.Container{{Role: "web", State: "running"}}}}
	rep, _ := Run(context.Background(), f.deps(), map[string]registry.Project{"app": mkProject(t, "app", reactYAML)})
	b, _ := json.Marshal(rep)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"checkedAt", "pods", "upToDate", "available", "unknown"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing %s in %s", k, b)
		}
	}
	p := m["pods"].([]any)[0].(map[string]any)
	for _, k := range []string{"name", "type", "domain", "state", "updateAvailable", "parts", "roles"} {
		if _, ok := p[k]; !ok {
			t.Fatalf("pod missing %s in %s", k, b)
		}
	}
	if _, ok := p["error"]; ok {
		t.Fatal("error should be omitted when empty")
	}
}

func TestRunBoundedConcurrency(t *testing.T) {
	f := newFakes()
	var mu sync.Mutex
	inflight, peak := 0, 0
	release := make(chan struct{})
	d := f.deps()
	d.ImageInspect = func(ctx context.Context, ref string) (engine.ImageInfo, error) {
		return engine.ImageInfo{ID: "x", Architecture: "arm64"}, nil
	}
	d.RemoteConfigDigest = func(ctx context.Context, ref Ref, arch string) (string, error) {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		<-release
		mu.Lock()
		inflight--
		mu.Unlock()
		return "x", nil
	}
	unique := map[string]struct{}{}
	for i := 0; i < 24; i++ {
		unique[fmt.Sprintf("ghcr.io/cyber5-io/img%d:1", i)] = struct{}{}
	}
	done := make(chan struct{})
	go func() { checkRefs(context.Background(), d, unique); close(done) }()
	for i := 0; i < 24; i++ {
		time.Sleep(5 * time.Millisecond)
		release <- struct{}{}
	}
	<-done
	// The literal 6 is the requirement; asserting against maxConcurrency
	// would mutate together with the code and never fail.
	if peak > 6 || peak < 2 {
		t.Fatalf("peak in-flight %d, want 2..6", peak)
	}
}

func TestRunMixedPodIsAvailableWithError(t *testing.T) {
	f := newFakes()
	sameEverywhere(f, web, db)
	f.local[wpA] = "old"
	f.digest[wpA] = "sha256:new"
	f.local[rA] = "x"
	f.pods = []pod.Pod{{Name: "blog", Containers: []pod.Container{{Role: "web", State: "running"}}}}
	f.rerr[db] = errors.New("registry unreachable")
	rep, err := Run(context.Background(), f.deps(), map[string]registry.Project{"blog": mkProject(t, "blog", wpYAML)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Available != 1 || rep.Unknown != 0 || rep.Pods[0].Error == "" {
		t.Fatalf("report %+v", rep)
	}
	if !strings.Contains(rep.Pods[0].Error, db) {
		t.Fatalf("error should name the ref: %q", rep.Pods[0].Error)
	}
}

func TestRunCancelledContext(t *testing.T) {
	f := newFakes()
	sameEverywhere(f, web, rA)
	f.pods = []pod.Pod{{Name: "app", Containers: []pod.Container{{Role: "web", State: "running"}}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, f.deps(), map[string]registry.Project{"app": mkProject(t, "app", reactYAML)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRunPerPodErrorCases(t *testing.T) {
	t.Run("manifest load failure", func(t *testing.T) {
		f := newFakes()
		f.pods = []pod.Pod{{Name: "bad", Containers: []pod.Container{{Role: "web", State: "running"}}}}
		rep, err := Run(context.Background(), f.deps(), map[string]registry.Project{"bad": {Path: t.TempDir(), Type: "react", Domain: "bad.tainer.me"}})
		if err != nil {
			t.Fatal(err)
		}
		p := rep.Pods[0]
		if rep.Unknown != 1 || p.Error == "" || p.Parts != "" || p.Roles == nil || len(p.Roles) != 0 {
			t.Fatalf("report %+v", rep)
		}
	})
	t.Run("unparsable ref", func(t *testing.T) {
		t.Setenv("TAINER_IMAGE_REPO", "nohostrepo@sha256:abc")
		f := newFakes()
		f.pods = []pod.Pod{{Name: "app", Containers: []pod.Container{{Role: "web", State: "running"}}}}
		rep, err := Run(context.Background(), f.deps(), map[string]registry.Project{"app": mkProject(t, "app", reactYAML)})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Unknown != 1 || rep.Pods[0].Error == "" || rep.Pods[0].UpdateAvailable {
			t.Fatalf("report %+v", rep)
		}
	})
	t.Run("stopped pod container without role", func(t *testing.T) {
		f := newFakes()
		sameEverywhere(f, web, rA)
		f.pods = []pod.Pod{{Name: "app", Containers: []pod.Container{{State: "exited"}}}}
		rep, err := Run(context.Background(), f.deps(), map[string]registry.Project{"app": mkProject(t, "app", reactYAML)})
		if err != nil {
			t.Fatal(err)
		}
		if rep.UpToDate != 1 || rep.Pods[0].State != "stopped" {
			t.Fatalf("report %+v", rep)
		}
	})
}

func TestRunUpdatedWhileStoppedStaysInReport(t *testing.T) {
	f := newFakes()
	f.pods = nil
	rep, err := Run(context.Background(), f.deps(), map[string]registry.Project{"blog": mkProject(t, "blog", wpYAML)})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Pods) != 1 || rep.UpToDate != 1 || rep.Pods[0].State != "stopped" || rep.Pods[0].UpdateAvailable {
		t.Fatalf("report %+v", rep)
	}
}
