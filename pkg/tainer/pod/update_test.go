package pod

import (
	"errors"
	"strings"
	"testing"
)

func TestRestartAfterUpdate(t *testing.T) {
	running := Container{Name: "a", State: "running"}
	exited := Container{Name: "b", State: "exited"}
	cases := []struct {
		name string
		pod  *Pod
		want bool
	}{
		{"nil pod is never booted", nil, false},
		{"never started", &Pod{}, false},
		{"running", &Pod{Containers: []Container{running, running}}, true},
		{"mixed counts as running", &Pod{Containers: []Container{running, exited}}, true},
		{"stopped stays stopped", &Pod{Containers: []Container{exited, exited}}, false},
	}
	for _, c := range cases {
		if got := restartAfterUpdate(c.pod); got != c.want {
			t.Errorf("%s: restartAfterUpdate = %v, want %v", c.name, got, c.want)
		}
	}
}

type recorder struct {
	calls   []string
	stopErr error
}

func (r *recorder) ops() recreateOps {
	return recreateOps{
		stop:   func() error { r.calls = append(r.calls, "stop"); return r.stopErr },
		remove: func(n string) error { r.calls = append(r.calls, "rm:"+n); return nil },
		start:  func() error { r.calls = append(r.calls, "start"); return nil },
	}
}

func noReport(string, string, int, bool) {}

func TestRecreate(t *testing.T) {
	running := Container{Name: "a", State: "running"}
	exited := Container{Name: "b", State: "exited"}
	cases := []struct {
		name        string
		prior       *Pod
		wantCalls   string
		wantStarted bool
	}{
		{"nil pod is a no-op", nil, "", false},
		{"never started has no containers", &Pod{}, "", false},
		{"stopped: remove only, no stop, no start", &Pod{Containers: []Container{exited}}, "rm:b", false},
		{"running: stop, remove, start", &Pod{Containers: []Container{running}}, "stop rm:a start", true},
		{"mixed: restored", &Pod{Containers: []Container{running, exited}}, "stop rm:a rm:b start", true},
	}
	for _, c := range cases {
		r := &recorder{}
		res, err := recreate(c.prior, noReport, r.ops())
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if got := strings.Join(r.calls, " "); got != c.wantCalls {
			t.Errorf("%s: calls = %q, want %q", c.name, got, c.wantCalls)
		}
		if res.Started != c.wantStarted {
			t.Errorf("%s: Started = %v, want %v", c.name, res.Started, c.wantStarted)
		}
	}
}

func TestRecreateWrapsRemoveError(t *testing.T) {
	ops := recreateOps{
		stop:   func() error { return nil },
		remove: func(string) error { return errors.New("busy") },
		start:  func() error { t.Fatal("start must not run"); return nil },
	}
	_, err := recreate(&Pod{Containers: []Container{{Name: "a", State: "running"}}}, noReport, ops)
	if err == nil || !strings.Contains(err.Error(), "update: remove a: busy") {
		t.Fatalf("err = %v", err)
	}
}
