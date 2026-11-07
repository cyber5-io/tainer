package pod

import (
	"errors"
	"strings"
	"testing"
)

// rawBusy is the engine error seen in tain-101.
var rawBusy = errors.New(`engine: remove tainer-control-app: Error response from daemon: replacing mount point "/var/lib/cyberstack/storage/overlay/abc/merged": device or resource busy`)

func TestRemoveErrBusyBecomesPlainMessage(t *testing.T) {
	for _, raw := range []error{rawBusy, errors.New("engine: remove x: unlinkat /x: device or resource busy")} {
		err := removeErr("control", Container{Name: "tainer-control-app", Role: "app"}, "replace", raw)
		want := `couldn't replace control's app — its files are still in use. Run "tainer restart control" or try again in a minute; if it keeps happening, restart tainer from the menu bar.`
		if err == nil || err.Error() != want {
			t.Fatalf("err = %q\nwant  %q", err, want)
		}
		var busy *BusyRemovalError
		if !errors.As(err, &busy) || !errors.Is(err, raw) || busy.Detail() != raw.Error() {
			t.Fatalf("raw error must stay reachable: %#v", err)
		}
		for _, banned := range []string{"podman", "docker", "engine", "VM", "container", "mount"} {
			if strings.Contains(err.Error(), banned) {
				t.Errorf("user message contains %q: %s", banned, err)
			}
		}
	}
}

func TestRemoveErrRoleFallsBackToName(t *testing.T) {
	err := removeErr("shop", Container{Name: "tainer-shop-db"}, "remove", rawBusy)
	if err == nil || !strings.HasPrefix(err.Error(), "couldn't remove shop's db — ") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveErrOtherErrorsKeepContext(t *testing.T) {
	raw := errors.New("permission denied")
	err := removeErr("shop", Container{Name: "tainer-shop-app", Role: "app"}, "replace", raw)
	if err == nil || err.Error() != "remove tainer-shop-app: permission denied" || !errors.Is(err, raw) {
		t.Fatalf("err = %v", err)
	}
	var busy *BusyRemovalError
	if errors.As(err, &busy) {
		t.Fatal("non-busy error must not become BusyRemovalError")
	}
	if removeErr("shop", Container{Name: "a"}, "replace", nil) != nil {
		t.Fatal("nil in, nil out")
	}
}

func TestRecreateBusyRemoveGetsPlainMessage(t *testing.T) {
	ops := recreateOps{
		stop:   func() error { return nil },
		remove: func(string) error { return rawBusy },
		start:  func() error { t.Fatal("start must not run"); return nil },
	}
	prior := &Pod{Name: "control", Containers: []Container{{Name: "tainer-control-app", Role: "app", State: "running"}}}
	_, err := recreate(prior, noReport, ops)
	var busy *BusyRemovalError
	if !errors.As(err, &busy) || !strings.HasPrefix(err.Error(), "couldn't replace control's app — ") {
		t.Fatalf("err = %v", err)
	}
}
