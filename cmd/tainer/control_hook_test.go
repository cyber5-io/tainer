package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// hookRegistered records whether init() wired the runtime-ready hook. The
// hook is then disabled for the whole test binary so no other test can reach
// the user's real runtime or config through it.
var hookRegistered bool

func TestMain(m *testing.M) {
	hookRegistered = runtime.OnReady != nil
	runtime.OnReady = nil
	os.Exit(m.Run())
}

// hookFake swaps every seam the control hook uses and counts calls.
type hookFake struct {
	boot       string
	present    bool
	ensureErr  error
	ensureWait bool // block until the ensure context is done
	ensures    int
	presence   int
	launches   int
	launchBoot string
	now        time.Time
}

func newHookFake(t *testing.T) *hookFake {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := &hookFake{boot: "pid:100@1", present: true, now: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	oldBoot, oldPresent, oldEnsure, oldLaunch, oldNow, oldBudget := controlBootID, controlImagesPresent, ensureControlPod, launchControlEnsure, controlNow, controlSyncBudget
	t.Cleanup(func() {
		controlBootID, controlImagesPresent, ensureControlPod, launchControlEnsure, controlNow, controlSyncBudget = oldBoot, oldPresent, oldEnsure, oldLaunch, oldNow, oldBudget
	})
	controlBootID = func(*engine.Client) string { return f.boot }
	controlImagesPresent = func(context.Context, *engine.Client) bool { f.presence++; return f.present }
	ensureControlPod = func(ctx context.Context, _ *engine.Client) error {
		f.ensures++
		if f.ensureWait {
			<-ctx.Done()
			return fmt.Errorf("%w: %w", pod.ErrControlStart, ctx.Err())
		}
		return f.ensureErr
	}
	launchControlEnsure = func(boot string) error { f.launches++; f.launchBoot = boot; return nil }
	controlNow = func() time.Time { return f.now }
	return f
}

func (f *hookFake) engineTouches() int { return f.ensures + f.presence + f.launches }

func ready(ctx context.Context, w *bytes.Buffer) {
	onRuntimeReady(ctx, &engine.Client{}, w)
}

var bannedWords = []string{"podman", "docker", "engine", "vm", "container"}

func TestControlHookIsRegistered(t *testing.T) {
	if !hookRegistered {
		t.Fatal("runtime.OnReady must be registered by the tainer command")
	}
}

func TestEnsuresOncePerDaemonBoot(t *testing.T) {
	f := newHookFake(t)
	var w bytes.Buffer
	ready(context.Background(), &w)
	if f.ensures != 1 {
		t.Fatalf("first call: ensures=%d, want 1", f.ensures)
	}
	before := f.engineTouches()
	ready(context.Background(), &w)
	ready(context.Background(), &w)
	if f.engineTouches() != before {
		t.Errorf("an ensured boot must not touch the runtime again (touches %d → %d)", before, f.engineTouches())
	}
	f.boot = "pid:200@2" // the daemon restarted
	ready(context.Background(), &w)
	if f.ensures != 2 {
		t.Errorf("a new daemon boot must be ensured again: ensures=%d", f.ensures)
	}
	if w.Len() != 0 {
		t.Errorf("success must be silent, got %q", w.String())
	}
}

func TestMissingImagesLaunchADetachedChild(t *testing.T) {
	f := newHookFake(t)
	f.present = false
	var w bytes.Buffer
	start := time.Now()
	ready(context.Background(), &w)
	if time.Since(start) > time.Second {
		t.Errorf("hook took %v with images missing; it must hand off and return", time.Since(start))
	}
	if f.launches != 1 || f.ensures != 0 {
		t.Errorf("launches=%d ensures=%d, want 1/0", f.launches, f.ensures)
	}
	// While the child works, later commands neither relaunch nor block.
	ready(context.Background(), &w)
	if f.launches != 1 || f.ensures != 0 {
		t.Errorf("relaunched while the child is running: launches=%d ensures=%d", f.launches, f.ensures)
	}
	if w.Len() != 0 {
		t.Errorf("a hand-off is silent, got %q", w.String())
	}
}

func TestCallerKeepsItsBudget(t *testing.T) {
	f := newHookFake(t)
	f.ensureWait = true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var w bytes.Buffer
	start := time.Now()
	ready(ctx, &w)
	if el := time.Since(start); el > 200*time.Millisecond {
		t.Errorf("hook took %v of a deadline-bound caller's time; it must hand off at once", el)
	}
	if dl, _ := ctx.Deadline(); time.Until(dl) < 29*time.Second {
		t.Errorf("caller left with %v of 30s", time.Until(dl))
	}
	if f.ensures != 0 || f.presence != 0 || f.launches != 1 || w.Len() != 0 {
		t.Errorf("deadline-bound caller: ensures=%d presence=%d launches=%d stderr=%q, want 0/0/1/silent", f.ensures, f.presence, f.launches, w.String())
	}
}

func TestNoDeadlineRunsInProcessWithinItsBound(t *testing.T) {
	f := newHookFake(t)
	f.ensureWait = true
	controlSyncBudget = 50 * time.Millisecond
	var w bytes.Buffer
	start := time.Now()
	ready(context.Background(), &w)
	if el := time.Since(start); el > time.Second {
		t.Errorf("in-process ensure ran %v, want it bounded by the sync budget", el)
	}
	if f.ensures != 1 || f.launches != 1 || w.Len() != 0 {
		t.Errorf("cut off by its bound: ensures=%d launches=%d stderr=%q, want 1/1/silent", f.ensures, f.launches, w.String())
	}
}

func TestChildGetsTheParentsBoot(t *testing.T) {
	f := newHookFake(t)
	f.present = false
	var w bytes.Buffer
	ready(context.Background(), &w)
	if f.launchBoot != f.boot {
		t.Errorf("child launched with boot %q, want %q", f.launchBoot, f.boot)
	}
}

func TestChildConnectFailureKeepsTheThrottle(t *testing.T) {
	f := newHookFake(t)
	old := controlEngine
	t.Cleanup(func() { controlEngine = old })
	controlEngine = func(context.Context) (*engine.Client, error) { return nil, errors.New("unreachable") }

	// With the parent's boot: the failure is recorded against it.
	t.Setenv(controlBootEnv, f.boot)
	writeControlState(controlState{AttemptBoot: f.boot, LastAttempt: f.now})
	runControlEnsure()
	if st := readControlState(); st.AttemptBoot != f.boot || st.LastError != "other" {
		t.Errorf("with parent boot: state = %+v", st)
	}
	var w bytes.Buffer
	ready(context.Background(), &w)
	if f.launches != 0 || f.ensures != 0 {
		t.Errorf("retried inside the window: launches=%d ensures=%d", f.launches, f.ensures)
	}

	// Without one: AttemptBoot is left as the parent wrote it.
	t.Setenv(controlBootEnv, "")
	writeControlState(controlState{AttemptBoot: "pid:7@7", LastAttempt: f.now})
	runControlEnsure()
	if st := readControlState(); st.AttemptBoot != "pid:7@7" {
		t.Errorf("unknown boot clobbered AttemptBoot: %+v", st)
	}
}

func TestShortCallerDeadlineHandsOff(t *testing.T) {
	f := newHookFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var w bytes.Buffer
	ready(ctx, &w)
	if f.ensures != 0 || f.launches != 1 {
		t.Errorf("short deadline: ensures=%d launches=%d, want 0/1 (no synchronous work)", f.ensures, f.launches)
	}
}

func TestFailedAttemptsAreThrottled(t *testing.T) {
	f := newHookFake(t)
	f.ensureErr = fmt.Errorf("%w: boom", pod.ErrControlStart)
	var w bytes.Buffer
	ready(context.Background(), &w)
	f.now = f.now.Add(9 * time.Minute)
	ready(context.Background(), &w)
	if f.ensures != 1 {
		t.Errorf("retried within 10 minutes: ensures=%d", f.ensures)
	}
	f.now = f.now.Add(2 * time.Minute)
	ready(context.Background(), &w)
	if f.ensures != 2 {
		t.Errorf("no retry after 10 minutes: ensures=%d", f.ensures)
	}
	f.ensureErr = nil
	f.now = f.now.Add(11 * time.Minute)
	ready(context.Background(), &w)
	ready(context.Background(), &w)
	if f.ensures != 3 {
		t.Errorf("after a success the boot is ensured: ensures=%d, want 3", f.ensures)
	}
}

func TestWarnsOncePerDaemonBoot(t *testing.T) {
	f := newHookFake(t)
	f.ensureErr = fmt.Errorf("%w: boom", pod.ErrControlStart)
	var w bytes.Buffer
	ready(context.Background(), &w)
	ready(context.Background(), &w)
	f.now = f.now.Add(11 * time.Minute)
	ready(context.Background(), &w)
	if n := strings.Count(w.String(), "\n"); n != 1 {
		t.Fatalf("want one warning for this boot, got %d: %q", n, w.String())
	}
	want := "tainer: couldn't start the control dashboard yet (it didn't start); it will retry next time tainer starts\n"
	if w.String() != want {
		t.Errorf("warning = %q\nwant      %q", w.String(), want)
	}
	f.boot = "pid:300@3"
	ready(context.Background(), &w)
	if n := strings.Count(w.String(), "\n"); n != 2 {
		t.Errorf("a new boot warns again: %d lines", n)
	}
}

func TestOfflineWarningSpeaksPlainly(t *testing.T) {
	f := newHookFake(t)
	// Shaped like engine.Client.Pull's error, wrapped by EnsureControlPod.
	pullErr := errors.New("engine: pull ghcr.io/cyber5-io/tainer-control:1.0.0: Get \"https://ghcr.io/v2/\": dial tcp: lookup ghcr.io: no such host (container runtime)")
	f.ensureErr = fmt.Errorf("%w: app: %w", pod.ErrControlDownload, pullErr)
	var w bytes.Buffer
	ready(context.Background(), &w)
	got := w.String()
	want := "tainer: couldn't start the control dashboard yet (couldn't download it — are you offline?); it will retry next time tainer starts\n"
	if got != want {
		t.Errorf("warning = %q\nwant      %q", got, want)
	}
	for _, word := range bannedWords {
		if strings.Contains(strings.ToLower(got), word) {
			t.Errorf("warning says %q: %q", word, got)
		}
	}
}

func TestUnexpectedErrorIsNotEchoed(t *testing.T) {
	f := newHookFake(t)
	f.ensureErr = errors.New("engine: inspect tainer-control-web: container exploded")
	var w bytes.Buffer
	ready(context.Background(), &w)
	if !strings.Contains(w.String(), "(unexpected error)") || strings.Contains(w.String(), "exploded") {
		t.Errorf("warning = %q", w.String())
	}
}

func TestChildFailureIsReportedOnceByALaterCommand(t *testing.T) {
	f := newHookFake(t)
	f.present = false
	var w bytes.Buffer
	ready(context.Background(), &w) // hands off to the child
	// The child fails to download.
	recordControlFailure(f.boot, fmt.Errorf("%w: offline", pod.ErrControlDownload))
	ready(context.Background(), &w)
	ready(context.Background(), &w)
	want := "tainer: couldn't start the control dashboard yet (couldn't download it — are you offline?); it will retry next time tainer starts\n"
	if w.String() != want {
		t.Errorf("stderr = %q\nwant     %q", w.String(), want)
	}
	if f.launches != 1 {
		t.Errorf("relaunched inside the retry window: %d", f.launches)
	}
}

func TestChildEnsureRecordsSuccess(t *testing.T) {
	f := newHookFake(t)
	old := controlEngine
	t.Cleanup(func() { controlEngine = old })
	controlEngine = func(context.Context) (*engine.Client, error) { return &engine.Client{}, nil }
	if code := runControlEnsure(); code != 0 {
		t.Fatalf("runControlEnsure = %d", code)
	}
	if f.ensures != 1 {
		t.Fatalf("child ensures=%d, want 1", f.ensures)
	}
	var w bytes.Buffer
	ready(context.Background(), &w)
	if f.ensures != 1 || f.launches != 0 {
		t.Errorf("after the child succeeded the boot is ensured: ensures=%d launches=%d", f.ensures, f.launches)
	}
}

func TestOnlyOneChildRuns(t *testing.T) {
	newHookFake(t)
	lock := filepath.Join(t.TempDir(), "control-ensure.lock")
	release, ok, err := tryLock(lock)
	if err != nil || !ok {
		t.Fatalf("first lock: ok=%v err=%v", ok, err)
	}
	defer release()
	if _, ok2, _ := tryLock(lock); ok2 {
		t.Error("a second child took the lock while the first holds it")
	}
}

func TestInProcessEnsureSkipsWhileAnUpdateHoldsTheLock(t *testing.T) {
	f := newHookFake(t)
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	release, ok, err := tryLock(controlLockPath())
	if err != nil || !ok {
		t.Fatalf("hold lock: ok=%v err=%v", ok, err)
	}
	var w bytes.Buffer
	ready(context.Background(), &w)
	release()
	if f.ensures != 0 {
		t.Fatalf("ensure ran %d times while the lock was held", f.ensures)
	}
	if w.Len() != 0 {
		t.Errorf("no warning expected, got %q", w.String())
	}
}
