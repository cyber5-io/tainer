package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/release"
)

// fakeResume records every start and lets tests choose the installed
// version, the clock and per-pod failures.
type fakeResume struct {
	mu        sync.Mutex
	started   []string
	installed string
	now       time.Time
	fail      map[string]error
	out       bytes.Buffer
}

func (f *fakeResume) ops() resumeOps {
	return resumeOps{
		list: func(context.Context) ([]release.ResumeEntry, error) { return nil, nil },
		start: func(_ context.Context, e release.ResumeEntry) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.started = append(f.started, e.Name)
			return f.fail[e.Name]
		},
		installed: func() string { return f.installed },
		now:       func() time.Time { return f.now },
		out:       &f.out,
	}
}

func writeResumeFixture(t *testing.T, f release.ResumeFile) {
	t.Helper()
	if err := release.WriteResume(f); err != nil {
		t.Fatal(err)
	}
}

func resumeExists(t *testing.T) bool {
	t.Helper()
	_, ok, err := release.ReadResume()
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

var resumeT0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func twoPods() []release.ResumeEntry {
	return []release.ResumeEntry{{Name: "alpha", Path: "/p/alpha"}, {Name: "beta", Path: "/p/beta"}}
}

func TestResumeStartsOnlyRecordedPods(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeResumeFixture(t, release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: resumeT0, Pods: twoPods()})
	f := &fakeResume{installed: "1.5.0", now: resumeT0.Add(time.Minute)}
	if err := runResume(context.Background(), f.ops(), false); err != nil {
		t.Fatal(err)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(f.started, want) {
		t.Fatalf("started %v, want %v", f.started, want)
	}
	if resumeExists(t) {
		t.Fatal("resume file not cleared")
	}
	if !strings.Contains(f.out.String(), "alpha is back") || !strings.Contains(f.out.String(), "beta is back") {
		t.Fatalf("output %q", f.out.String())
	}
}

func TestResumeWithoutFileDoesNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := &fakeResume{installed: "1.5.0", now: resumeT0}
	if err := runResume(context.Background(), f.ops(), true); err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 0 {
		t.Fatalf("started %v", f.started)
	}
}

func TestResumeAfterCancelledInstallStartsRecordedPods(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeResumeFixture(t, release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: resumeT0, Pods: twoPods()})
	f := &fakeResume{installed: "1.4.0", now: resumeT0.Add(time.Minute)}
	if err := runResume(context.Background(), f.ops(), true); err != nil {
		t.Fatal(err)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(f.started, want) {
		t.Fatalf("started %v, want %v", f.started, want)
	}
	if resumeExists(t) {
		t.Fatal("resume file not cleared")
	}
}

func TestStaleResumeFileIsDiscarded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeResumeFixture(t, release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: resumeT0, Pods: twoPods()})
	f := &fakeResume{installed: "1.4.0", now: resumeT0.Add(time.Hour + time.Second)}
	if err := runResume(context.Background(), f.ops(), false); err != nil {
		t.Fatal(err)
	}
	if err := runResume(context.Background(), f.ops(), false); err != nil {
		t.Fatal(err)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(f.started, want) {
		t.Fatalf("started %v, want %v (exactly once)", f.started, want)
	}
	if resumeExists(t) {
		t.Fatal("resume file not cleared")
	}
}

func TestResumeLeavesFileWhileInstallMayBeOpen(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeResumeFixture(t, release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: resumeT0, Pods: twoPods()})
	f := &fakeResume{installed: "1.4.0", now: resumeT0.Add(59 * time.Minute)}
	if err := runResume(context.Background(), f.ops(), false); err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 0 {
		t.Fatalf("started %v while an install may be open", f.started)
	}
	if !resumeExists(t) {
		t.Fatal("resume file removed while an install may be open")
	}
}

func TestResumeRunsOnceUnderConcurrency(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeResumeFixture(t, release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: resumeT0, Pods: twoPods()})
	f := &fakeResume{installed: "1.5.0", now: resumeT0.Add(time.Minute)}
	ops := f.ops()
	// Slow starts widen the window in which a second resumer could race.
	inner := ops.start
	ops.start = func(ctx context.Context, e release.ResumeEntry) error {
		time.Sleep(20 * time.Millisecond)
		return inner(ctx, e)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = runResume(context.Background(), ops, false)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(f.started, want) {
		t.Fatalf("started %v, want %v (once per pod)", f.started, want)
	}
}

func TestResumeReportsFailuresAndContinues(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pods := []release.ResumeEntry{{Name: "alpha", Path: "/p/alpha"}, {Name: "beta", Path: "/p/beta"}, {Name: "gamma", Path: "/p/gamma"}}
	writeResumeFixture(t, release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: resumeT0, Pods: pods})
	f := &fakeResume{installed: "1.5.0", now: resumeT0, fail: map[string]error{"beta": errors.New("folder is gone")}}
	if err := runResume(context.Background(), f.ops(), true); err != nil {
		t.Fatalf("runResume should not fail when a pod fails: %v", err)
	}
	if want := []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(f.started, want) {
		t.Fatalf("started %v, want %v", f.started, want)
	}
	out := f.out.String()
	for _, line := range []string{"alpha is back", "beta didn't come back: folder is gone", "gamma is back"} {
		if !strings.Contains(out, line) {
			t.Fatalf("output %q missing %q", out, line)
		}
	}
	if resumeExists(t) {
		t.Fatal("resume file not cleared")
	}
}

func TestSnapshotSkipsStoppedAndPinned(t *testing.T) {
	pods := []pod.Pod{
		{Name: "web", Containers: []pod.Container{{State: "running"}, {State: "running"}}},
		{Name: "half", Containers: []pod.Container{{State: "running"}, {State: "exited"}}},
		{Name: "idle", Containers: []pod.Container{{State: "exited"}}},
		{Name: "control", Containers: []pod.Container{{State: "running"}}},
		{Name: "orphan", Containers: []pod.Container{{State: "running"}}},
	}
	projects := map[string]registry.Project{
		"web":     {Path: "/p/web"},
		"half":    {Path: "/p/half"},
		"idle":    {Path: "/p/idle"},
		"control": {Path: "/p/control"},
	}
	ops := resumeOps{list: func(context.Context) ([]release.ResumeEntry, error) {
		return runningEntries(pods, projects), nil
	}}
	got, err := snapshotRunning(context.Background(), ops)
	if err != nil {
		t.Fatal(err)
	}
	want := []release.ResumeEntry{{Name: "half", Path: "/p/half"}, {Name: "web", Path: "/p/web"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot %v, want %v", got, want)
	}
}
