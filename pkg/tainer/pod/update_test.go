package pod

import "testing"

func TestRestartAfterUpdate(t *testing.T) {
	running := Container{Name: "a", State: "running"}
	exited := Container{Name: "b", State: "exited"}
	cases := []struct {
		name string
		pod  *Pod
		want bool
	}{
		{"nil pod has nothing to preserve", nil, true},
		{"never started", &Pod{}, true},
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
