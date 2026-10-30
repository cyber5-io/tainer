package pod

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func TestBuildMountsAppMountsWholeProject(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
	}
	// The app container is the pod's dev shell (WordPress has an app
	// role), so it mounts the whole project at /var/www — giving an ssh
	// session the git repo, not just the docroot.
	mounts := buildMounts(m, "/p/demo", RoleApp)
	wantProject := false
	for _, mt := range mounts {
		if mt.Source == "/p/demo" && mt.Target == "/var/www" {
			wantProject = true
		}
		if mt.Source == "/p/demo/app" {
			t.Errorf("legacy /app source should not appear: %+v", mt)
		}
	}
	if !wantProject {
		t.Errorf("whole-project bind missing: %+v", mounts)
	}
}

func TestBuildMountsWebEdgeIsIsolated(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
	}
	// The web (caddy) container is the internet-facing edge — kept narrow
	// to the docroot + data, and must never see the repo.
	mounts := buildMounts(m, "/p/demo", RoleWeb)
	wantHTML, wantData, sawRepo := false, false, false
	for _, mt := range mounts {
		switch {
		case mt.Source == "/p/demo/html" && mt.Target == "/var/www/html":
			wantHTML = true
		case mt.Source == "/p/demo/data" && mt.Target == "/var/www/data":
			wantData = true
		case mt.Source == "/p/demo" && mt.Target == "/var/www":
			sawRepo = true
		}
	}
	if !wantHTML {
		t.Errorf("html bind missing: %+v", mounts)
	}
	if !wantData {
		t.Errorf("data bind missing: %+v", mounts)
	}
	if sawRepo {
		t.Errorf("web edge must not mount the whole project: %+v", mounts)
	}
}

func TestBuildMountsDBIsBindNotVolume(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
		Runtime: manifest.RuntimeConfig{Database: manifest.DatabaseMariaDB},
	}
	mounts := buildMounts(m, "/p/demo", RoleDB)
	if len(mounts) != 1 {
		t.Fatalf("db mounts: got %d, want 1", len(mounts))
	}
	mt := mounts[0]
	if mt.Source != "/p/demo/db" || mt.Target != "/var/lib/mysql" {
		t.Errorf("db mount: got %+v, want bind /p/demo/db -> /var/lib/mysql", mt)
	}
	if mt.Volume {
		t.Errorf("db mount must be a bind, not a named volume: %+v", mt)
	}
}

func TestBuildMountsDBPostgres(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeKompozi, Name: "blog"},
		Runtime: manifest.RuntimeConfig{Database: manifest.DatabasePostgres},
	}
	mounts := buildMounts(m, "/p/blog", RoleDB)
	if len(mounts) != 1 || mounts[0].Target != "/var/lib/postgresql/data" {
		t.Errorf("postgres db mount: got %+v, want target /var/lib/postgresql/data", mounts)
	}
}

func TestPodCgroupSettings(t *testing.T) {
	m := mkPod(manifest.PodSizeSmall) // helper from split_test.go (same package)
	memBytes, cpu, err := PodBudget(m)
	if err != nil {
		t.Fatal(err)
	}
	want := PodCgroup("demo")
	for _, role := range []string{RoleWeb, RoleApp, RoleDB} {
		parent, oom, res := podCgroupSettings("demo", role, memBytes, cpu)
		if parent != want {
			t.Errorf("%s CgroupParent = %q, want %q", role, parent, want)
		}
		if res.Memory != memBytes || res.NanoCPUs != int64(cpu*1e9) {
			t.Errorf("%s resources = %d/%d, want %d/%d (pod budget)", role, res.Memory, res.NanoCPUs, memBytes, int64(cpu*1e9))
		}
		if role == RoleDB && oom >= 0 {
			t.Errorf("db oomScoreAdj = %d, want negative (protect db)", oom)
		}
		if role != RoleDB && oom != 0 {
			t.Errorf("%s oomScoreAdj = %d, want 0", role, oom)
		}
	}
}

// TestBuildEnvReactBuildDir locks TAIN-98: the web (caddy) container of
// a react pod learns which bundle dir to serve via TAINER_BUILD_DIR —
// runtime.build-dir when the manifest sets it, dist otherwise.
func TestBuildEnvReactBuildDir(t *testing.T) {
	pctx := &podCtx{hostUID: 501, hostGID: 20}
	mk := func(buildDir string) *manifest.Manifest {
		return &manifest.Manifest{
			Project: manifest.ProjectConfig{Type: manifest.TypeReact},
			Runtime: manifest.RuntimeConfig{Node: "20", Database: manifest.DatabaseNone, BuildDir: buildDir},
		}
	}
	has := func(env []string, want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}

	if env := buildEnv(mk("build"), RoleWeb, pctx); !has(env, "TAINER_BUILD_DIR=build") {
		t.Errorf("react web env missing TAINER_BUILD_DIR=build: %v", env)
	}
	// With an app role in the pod, sshd belongs to app — web must not bind :22.
	if env := buildEnv(mk(""), RoleWeb, pctx); has(env, "TAINER_SSHD=1") {
		t.Errorf("react web env still claims sshd: %v", env)
	}
	if env := buildEnv(mk(""), RoleApp, pctx); !has(env, "TAINER_SSHD=1") {
		t.Errorf("react app env missing TAINER_SSHD=1: %v", env)
	}
	if env := buildEnv(mk(""), RoleWeb, pctx); !has(env, "TAINER_BUILD_DIR=dist") {
		t.Errorf("react web env missing TAINER_BUILD_DIR=dist default: %v", env)
	}
	// Non-react types don't get the env — their caddy configs proxy, not serve.
	mNode := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeNodeJS},
		Runtime: manifest.RuntimeConfig{Node: "20", Database: manifest.DatabaseNone},
	}
	for _, e := range buildEnv(mNode, RoleWeb, pctx) {
		if len(e) >= 17 && e[:17] == "TAINER_BUILD_DIR=" {
			t.Errorf("nodejs web env unexpectedly has %s", e)
		}
	}
}

// TestBuildEnvSvelteBuildDir: svelte is the second SPA type — same
// TAINER_BUILD_DIR contract as react (vite's dist by default,
// runtime.build-dir override).
func TestBuildEnvSvelteBuildDir(t *testing.T) {
	pctx := &podCtx{hostUID: 501, hostGID: 20}
	mk := func(buildDir string) *manifest.Manifest {
		return &manifest.Manifest{
			Project: manifest.ProjectConfig{Type: manifest.TypeSvelte},
			Runtime: manifest.RuntimeConfig{Node: "20", Database: manifest.DatabaseNone, BuildDir: buildDir},
		}
	}
	has := func(env []string, want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}

	if env := buildEnv(mk(""), RoleWeb, pctx); !has(env, "TAINER_BUILD_DIR=dist") {
		t.Errorf("svelte web env missing TAINER_BUILD_DIR=dist default: %v", env)
	}
	if env := buildEnv(mk("public"), RoleWeb, pctx); !has(env, "TAINER_BUILD_DIR=public") {
		t.Errorf("svelte web env missing TAINER_BUILD_DIR=public: %v", env)
	}
	if env := buildEnv(mk(""), RoleApp, pctx); !has(env, "TAINER_SSHD=1") {
		t.Errorf("svelte app env missing TAINER_SSHD=1: %v", env)
	}
}
