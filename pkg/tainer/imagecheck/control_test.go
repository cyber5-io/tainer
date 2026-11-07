package imagecheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

const stableRef = "ghcr.io/cyber5-io/tainer-control:stable"

// controlProject writes the built-in control definition to a temp folder and
// registers it with the recorded image version.
func controlProject(t *testing.T, recorded string) registry.Project {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := config.ControlDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), pod.ControlManifestYAML(), 0o644); err != nil {
		t.Fatal(err)
	}
	return registry.Project{Path: dir, ImageVersion: recorded}
}

// controlFakes is a fakes set where the control pod is running and the stable
// channel carries the given labels.
func controlFakes(labels map[string]string) (*fakes, *int) {
	f := newFakes()
	sameEverywhere(f, web)
	f.pods = []pod.Pod{{Name: "control", Containers: []pod.Container{{Role: "web", State: "running"}, {Role: "app", State: "running"}}}}
	calls := 0
	f.labels = func(ref Ref) (map[string]string, error) {
		calls++
		if got := ref.Host + "/" + ref.Repo + ":" + ref.Tag; got != stableRef {
			return nil, errors.New("unexpected label lookup for " + got)
		}
		return labels, nil
	}
	return f, &calls
}

func runControl(t *testing.T, f *fakes, recorded, tainerVersion string) (Report, PodReport) {
	t.Helper()
	d := f.deps()
	d.TainerVersion = tainerVersion
	rep, err := Run(context.Background(), d, map[string]registry.Project{"control": controlProject(t, recorded)})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Pods) != 1 || rep.Pods[0].Control == nil {
		t.Fatalf("want one control pod report with a control section, got %+v", rep.Pods)
	}
	return rep, rep.Pods[0]
}

func labelsFor(version, minVersion string) map[string]string {
	l := map[string]string{LabelVersion: version}
	if minVersion != "" {
		l[LabelMinVersion] = minVersion
	}
	return l
}

func TestControlNewerStableIsOffered(t *testing.T) {
	f, calls := controlFakes(labelsFor("1.2.0", "1.3.0"))
	rep, p := runControl(t, f, "", "1.3.0")
	if p.Control.State != ControlUpdate || p.Control.Running != "1.0.0" || p.Control.Stable != "1.2.0" {
		t.Fatalf("control = %+v", p.Control)
	}
	if !p.UpdateAvailable || rep.Available != 1 || rep.Held != 0 {
		t.Fatalf("pod not offered an update: %+v / %+v", p, rep)
	}
	if *calls != 1 {
		t.Errorf("stable labels read %d times, want 1", *calls)
	}
	// The app role is judged by version, never by re-checking its own tag.
	if n := f.remote["ghcr.io/cyber5-io/tainer-control:1.0.0"]; n != 0 {
		t.Errorf("versioned control tag digest-checked %d times", n)
	}
	var app *RoleReport
	for i := range p.Roles {
		if p.Roles[i].Role == "app" {
			app = &p.Roles[i]
		}
	}
	if app == nil || app.Image != "ghcr.io/cyber5-io/tainer-control:1.0.0" || !app.UpdateAvailable {
		t.Fatalf("app role = %+v", app)
	}
}

func TestControlNewerImageNeedingNewerTainerIsHeldBack(t *testing.T) {
	f, _ := controlFakes(labelsFor("1.2.0", "1.4.0"))
	rep, p := runControl(t, f, "1.1.0", "1.3.0")
	if p.Control.State != ControlNeedsNewerTainer || p.Control.MinTainer != "1.4.0" {
		t.Fatalf("control = %+v", p.Control)
	}
	if p.UpdateAvailable || rep.Available != 0 || rep.Held != 1 || rep.UpToDate != 0 || rep.Unknown != 0 {
		t.Fatalf("held image must not count as available: %+v / %+v", p, rep)
	}
}

func TestControlPinIsAFloorNotATarget(t *testing.T) {
	// recorded 1.3.0, pin 1.0.0, stable 1.2.0 → up to date, no downgrade.
	f, _ := controlFakes(labelsFor("1.2.0", ""))
	rep, p := runControl(t, f, "1.3.0", "1.3.0")
	if p.Control.State != ControlUpToDate || p.Control.Running != "1.3.0" {
		t.Fatalf("control = %+v", p.Control)
	}
	if p.UpdateAvailable || rep.UpToDate != 1 {
		t.Fatalf("stable older than running must be up to date: %+v / %+v", p, rep)
	}
}

func TestControlMissingMinVersionLabelIsAllowed(t *testing.T) {
	f, _ := controlFakes(labelsFor("1.2.0", ""))
	_, p := runControl(t, f, "", "1.3.0")
	if p.Control.State != ControlUpdate {
		t.Fatalf("control = %+v", p.Control)
	}
}

func TestControlCheckErrorsAreUnknown(t *testing.T) {
	f, _ := controlFakes(nil)
	f.labels = func(Ref) (map[string]string, error) { return nil, errors.New("offline") }
	rep, p := runControl(t, f, "", "1.3.0")
	if rep.Unknown != 1 || !strings.Contains(p.Error, "offline") {
		t.Fatalf("rep = %+v pod = %+v", rep, p)
	}
	if p.Control.State != ControlUnknown || p.Control.Running != "1.0.0" || p.UpdateAvailable {
		t.Fatalf("failed check control = %+v", p.Control)
	}

	f, _ = controlFakes(map[string]string{})
	rep, p = runControl(t, f, "", "1.3.0")
	if rep.Unknown != 1 || !strings.Contains(p.Error, "version") {
		t.Fatalf("missing version label: rep = %+v pod = %+v", rep, p)
	}
}

func TestControlDecision(t *testing.T) {
	cases := []struct {
		name                          string
		running, stable, min, tainerV string
		want                          string
		wantErr                       bool
	}{
		{"newer stable is offered", "1.0.0", "1.2.0", "1.3.0", "1.3.0", ControlUpdate, false},
		{"equal is up to date", "1.2.0", "1.2.0", "", "1.3.0", ControlUpToDate, false},
		{"pin is a floor, not a target", "1.3.0", "1.2.0", "", "1.3.0", ControlUpToDate, false},
		{"needs newer tainer", "1.0.0", "1.2.0", "1.4.0", "1.3.0", ControlNeedsNewerTainer, false},
		{"missing min-version label is allowed", "1.0.0", "1.2.0", "", "1.3.0", ControlUpdate, false},
		{"dev build is allowed", "1.0.0", "1.2.0", "9.0.0", "dev", ControlUpdate, false},
		{"empty tainer version is a dev build", "1.0.0", "1.2.0", "9.0.0", "", ControlUpdate, false},
		{"v-prefixed versions", "1.0.0", "v1.2.0", "v1.3.0", "v1.3.0", ControlUpdate, false},
		{"prerelease tainer below min", "1.0.0", "1.2.0", "1.4.0", "1.4.0-rc.1", ControlNeedsNewerTainer, false},
		{"bad stable version", "1.0.0", "latest", "", "1.3.0", "", true},
		{"bad min-version", "1.0.0", "1.2.0", "soon", "1.3.0", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ControlDecision(c.running, c.stable, c.min, c.tainerV)
			if (err != nil) != c.wantErr || got != c.want {
				t.Fatalf("ControlDecision = %q, %v; want %q (err %v)", got, err, c.want, c.wantErr)
			}
		})
	}
}

func TestCheckControlReadsStableLabels(t *testing.T) {
	var seen Ref
	d := Deps{
		TainerVersion: "1.3.0",
		RemoteLabels: func(_ context.Context, ref Ref, _ string) (map[string]string, error) {
			seen = ref
			return labelsFor("1.1.0", "1.2.0"), nil
		},
	}
	c, err := CheckControl(context.Background(), d, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if seen.Tag != "stable" || seen.Repo != "cyber5-io/tainer-control" {
		t.Errorf("looked up %+v", seen)
	}
	want := ControlCheck{Running: "1.0.0", Stable: "1.1.0", MinTainer: "1.2.0", State: ControlUpdate}
	if c != want {
		t.Fatalf("CheckControl = %+v, want %+v", c, want)
	}
}

// The check reports the version the app actually runs, read from its image
// tag: a tainer upgrade that raised the pin, before the pod moved to it, is an
// update — not "up to date" on the strength of the recorded version.
func TestControlRunningIsReadFromTheAppImage(t *testing.T) {
	f, _ := controlFakes(labelsFor("1.0.0", ""))
	f.pods[0].Containers[1].Image = "ghcr.io/cyber5-io/tainer-control:0.9.0"
	rep, p := runControl(t, f, "", "1.3.0")
	if p.Control.Running != "0.9.0" || p.Control.State != ControlUpdate || rep.Available != 1 {
		t.Fatalf("control = %+v, available %d; want running 0.9.0 offered 1.0.0", p.Control, rep.Available)
	}
	for _, r := range p.Roles {
		if r.Role == "app" && r.Image != "ghcr.io/cyber5-io/tainer-control:0.9.0" {
			t.Errorf("app role image = %q, want what runs", r.Image)
		}
	}
}

func TestControlRunningFallsBackWithoutAVersionTag(t *testing.T) {
	f, _ := controlFakes(labelsFor("1.2.0", ""))
	f.pods[0].Containers[1].Image = "ghcr.io/cyber5-io/tainer-control:stable"
	_, p := runControl(t, f, "1.2.0", "1.3.0")
	if p.Control.Running != "1.2.0" || p.Control.State != ControlUpToDate {
		t.Fatalf("control = %+v, want the recorded 1.2.0", p.Control)
	}
}

// A user's own project named control (registered elsewhere) is checked like
// any pod, not as the dashboard.
func TestAControlProjectElsewhereIsNotTheDashboard(t *testing.T) {
	f, calls := controlFakes(labelsFor("1.2.0", ""))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	body := "version: 2\nproject:\n  name: control\n  type: php\n  domain: control.test\nruntime:\n  php: \"8.3\"\n  database: mariadb\n"
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(context.Background(), f.deps(), map[string]registry.Project{"control": {Path: dir}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Pods) != 1 || rep.Pods[0].Control != nil || *calls != 0 {
		t.Fatalf("user's control checked as the dashboard: %+v (label reads %d)", rep.Pods, *calls)
	}
}
