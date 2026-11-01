package pod

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

func TestControlPodGetsGatewayMounts(t *testing.T) {
	mounts, env := gatewayInjection("control", RoleApp, "/home/u/.cyberstack", "/p/control")
	if len(mounts) != 1 {
		t.Fatalf("want 1 mount, got %+v", mounts)
	}
	mt := mounts[0]
	if mt.Source != "/home/u/.cyberstack/control" || mt.Target != "/run/tainer/control" || !mt.ReadOnly || mt.Volume {
		t.Errorf("bad state mount: %+v", mt)
	}
	want := map[string]bool{
		"TAINER_GATEWAY_ADDR_FILE=/run/tainer/control/control.addr":   false,
		"TAINER_CONTROL_TOKEN_FILE=/run/tainer/control/control.token": false,
	}
	for _, e := range env {
		if _, ok := want[e]; ok {
			want[e] = true
		}
	}
	want["TAINER_PROJECT_PATH=/p/control"] = false
	for _, e := range env {
		if _, ok := want[e]; ok {
			want[e] = true
		}
	}
	for e, seen := range want {
		if !seen {
			t.Errorf("env %q missing: %v", e, env)
		}
	}
}

func TestGatewayInjectionSkipped(t *testing.T) {
	cases := []struct{ project, role, dir string }{
		{"blog", RoleApp, "/home/u/.cyberstack"},
		{"control", RoleWeb, "/home/u/.cyberstack"},
		{"control", RoleApp, ""},
	}
	for _, c := range cases {
		mounts, env := gatewayInjection(c.project, c.role, c.dir, "/p/x")
		if len(mounts) != 0 || len(env) != 0 {
			t.Errorf("%+v: expected no injection, got %+v %v", c, mounts, env)
		}
	}
}

func TestGatewayNeverMountsStateRoot(t *testing.T) {
	mounts, _ := gatewayInjection("control", RoleApp, "/home/u/.cyberstack", "/p/control")
	for _, mt := range mounts {
		if mt.Source == "/home/u/.cyberstack" {
			t.Errorf("state directory root must never be mounted: %+v", mt)
		}
	}
}

func TestEnsureGatewayDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "control")
	ensureGatewayDirs([]engine.Mount{{Source: dir}})
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("want 0700 dir, got %v %v", fi, err)
	}
}
