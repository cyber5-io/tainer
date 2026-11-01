package pod

import "testing"

func TestControlPodGetsGatewayMounts(t *testing.T) {
	mounts, env := gatewayInjection("control", RoleApp, "/home/u/.cyberstack")
	if len(mounts) != 1 {
		t.Fatalf("want 1 mount, got %+v", mounts)
	}
	mt := mounts[0]
	if mt.Source != "/home/u/.cyberstack" || mt.Target != "/run/tainer/state" || !mt.ReadOnly || mt.Volume {
		t.Errorf("bad state mount: %+v", mt)
	}
	want := map[string]bool{
		"TAINER_GATEWAY_ADDR_FILE=/run/tainer/state/control.addr":   false,
		"TAINER_CONTROL_TOKEN_FILE=/run/tainer/state/control.token": false,
	}
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
		mounts, env := gatewayInjection(c.project, c.role, c.dir)
		if len(mounts) != 0 || len(env) != 0 {
			t.Errorf("%+v: expected no injection, got %+v %v", c, mounts, env)
		}
	}
}
