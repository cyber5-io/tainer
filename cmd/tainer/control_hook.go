package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// The control pod is ensured once per daemon boot. Every successful
// runtime.Engine call runs onRuntimeReady, which on the common path is a
// single small file read: the boot is already ensured, nothing to do.
//
// For a boot not yet ensured:
//   - the caller's context has a deadline → a detached hidden
//     `tainer control ensure` child does it on its own budget and the caller
//     returns immediately (deadline-bound callers such as a network switch
//     must keep their whole budget);
//   - no deadline and the control images are local → EnsureControlPod runs
//     in-process, bounded by controlSyncBudget;
//   - images missing (first run, an update) or that bound ran out → the
//     detached child takes over.
//
// Failed attempts are retried at most every controlRetryEvery per boot, and
// the one-line warning is printed at most once per boot.
const (
	controlRetryEvery  = 10 * time.Minute
	controlChildBudget = 5 * time.Minute
	// controlBootEnv carries the parent's boot ID to the child, so a child
	// that can't connect still records its failure against the right boot.
	controlBootEnv = "TAINER_CONTROL_BOOT"
)

// Seams for tests.
var (
	controlSyncBudget    = 20 * time.Second
	controlBootID        = daemonBootID
	controlImagesPresent = pod.ControlImagesPresent
	ensureControlPod     = pod.EnsureControlPod
	launchControlEnsure  = launchControlEnsureChild
	controlNow           = time.Now
	controlEngine        = func(ctx context.Context) (*engine.Client, error) {
		return runtime.Engine(ctx, runtime.Options{})
	}
)

func init() {
	runtime.OnReady = func(ctx context.Context, cli *engine.Client) {
		onRuntimeReady(ctx, cli, os.Stderr)
	}
}

// controlState is the stamp file at <BaseDir>/control-state.json. Boots are
// identified by daemonBootID.
type controlState struct {
	EnsuredBoot string    `json:"ensuredBoot,omitempty"` // boot the pod was last ensured for
	AttemptBoot string    `json:"attemptBoot,omitempty"` // boot of the last unfinished/failed attempt
	LastAttempt time.Time `json:"lastAttempt,omitempty"`
	LastError   string    `json:"lastError,omitempty"` // failure class, see controlReason
	// Conflict is the folder of the user's own project named control, when
	// that is what keeps the dashboard from being set up (class "name-taken").
	Conflict   string `json:"conflict,omitempty"`
	WarnedBoot string `json:"warnedBoot,omitempty"`
	// Recreating is set while something has the control pod torn down (see
	// recreateControl). Still set with the control lock free means that
	// recreate was cut short, and the next command brings the pod back.
	Recreating bool `json:"recreating,omitempty"`
}

// controlLockPath serialises everything that recreates the control pod: the
// ensure (in-process or the detached child) and a control update.
func controlLockPath() string { return filepath.Join(config.BaseDir(), "control-ensure.lock") }

// controlLocker takes the control lock. ok is false when the caller should
// skip its work because another process holds the lock.
type controlLocker func(ctx context.Context) (release func(), ok bool, err error)

// waitForControlLock waits for the control lock (updates, dev changes).
func waitForControlLock(ctx context.Context) (func(), bool, error) {
	release, err := waitControlLock(ctx)
	return release, err == nil, err
}

// tryControlLock takes the control lock only if it is free (the ensure).
func tryControlLock(context.Context) (func(), bool, error) {
	if err := config.EnsureDirs(); err != nil {
		return nil, false, err
	}
	return tryLock(controlLockPath())
}

// controlLockFree reports whether nothing holds the control lock right now.
func controlLockFree() bool {
	release, ok, err := tryLock(controlLockPath())
	if err != nil || !ok {
		return false
	}
	release()
	return true
}

// recreateControl is the one way anything tears the control pod down and
// brings it back — an update, a dev approval change, the once-per-boot
// ensure. Holding the control lock, it clears this boot's "ensured" stamp
// before fn runs and sets it again only when fn succeeds, so a recreate that
// fails or is cut short (the process killed between remove and start) is
// redone by the next tainer command instead of leaving my.tainer.me down
// until the next runtime boot. retryNow also lifts the retry throttle after a
// failure (an ensure records its own failure and keeps the throttle). ran is
// false when lock says another process holds the lock.
func recreateControl(ctx context.Context, boot string, lock controlLocker, retryNow bool, fn func(context.Context) error) (ran bool, err error) {
	release, ok, err := lock(ctx)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	defer release()

	st := readControlState()
	st.EnsuredBoot, st.Recreating = "", true
	writeControlState(st)

	err = fn(ctx)

	st = readControlState()
	st.Recreating = false
	if err == nil {
		st.EnsuredBoot, st.LastError, st.Conflict = boot, "", ""
	} else if retryNow {
		st.AttemptBoot = ""
	}
	writeControlState(st)
	return true, err
}

// waitControlLock blocks until it holds the control lock or ctx ends.
func waitControlLock(ctx context.Context) (func(), error) {
	if err := config.EnsureDirs(); err != nil {
		return nil, err
	}
	for {
		release, ok, err := tryLock(controlLockPath())
		if err != nil {
			return nil, err
		}
		if ok {
			return release, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for the control dashboard to finish starting: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func controlStatePath() string { return filepath.Join(config.BaseDir(), "control-state.json") }

func readControlState() controlState {
	var st controlState
	if b, err := os.ReadFile(controlStatePath()); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func writeControlState(st controlState) {
	if err := config.EnsureDirs(); err != nil {
		return
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	path := controlStatePath()
	tmp := path + ".partial"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
	}
}

// daemonBootID identifies the running daemon: its PID plus the PID file's
// mtime (guards against PID reuse), falling back to the socket's mtime where
// no PID file is readable (Linux, systemd-owned). Empty when neither exists.
func daemonBootID(cli *engine.Client) string {
	socket := strings.TrimPrefix(cli.Socket(), "unix://")
	if socket == "" {
		return ""
	}
	if pid := runtime.ReadPID(socket); pid > 0 {
		id := "pid:" + strconv.Itoa(pid)
		if st, err := os.Stat(filepath.Join(filepath.Dir(socket), "cyberstackd.pid")); err == nil {
			id += "@" + strconv.FormatInt(st.ModTime().UnixNano(), 10)
		}
		return id
	}
	if st, err := os.Stat(socket); err == nil {
		return "sock:" + strconv.FormatInt(st.ModTime().UnixNano(), 10)
	}
	return ""
}

// onRuntimeReady keeps the control pod up once per daemon boot. It never
// fails the caller's command and never spends the caller's deadline.
func onRuntimeReady(ctx context.Context, cli *engine.Client, w io.Writer) {
	boot := controlBootID(cli)
	st := readControlState()
	if boot != "" && st.EnsuredBoot == boot {
		return
	}
	if st.Recreating && controlLockFree() {
		// A recreate was cut short with the pod down: bring it back now.
		st.AttemptBoot = ""
	}
	now := controlNow()
	if st.AttemptBoot == boot && now.Sub(st.LastAttempt) < controlRetryEvery {
		// An attempt for this boot is running in a child or failed recently.
		if st.LastError != "" && st.WarnedBoot != boot {
			warnControl(w, st)
			st.WarnedBoot = boot
			writeControlState(st)
		}
		return
	}

	st.AttemptBoot, st.LastAttempt, st.LastError = boot, now, ""
	writeControlState(st)

	if ensureInProcess(ctx, cli, boot, w) {
		return
	}
	if err := launchControlEnsure(boot); err != nil {
		failControl(w, boot, err)
	}
}

// ensureInProcess runs EnsureControlPod synchronously when the caller has no
// deadline and the images are local. It returns false when the work should go
// to the detached child instead: a deadline-bound caller, images missing, or
// only our own short bound ran out (a slow cold start).
func ensureInProcess(ctx context.Context, cli *engine.Client, boot string, w io.Writer) bool {
	if _, bounded := ctx.Deadline(); bounded {
		return false
	}
	sctx, cancel := context.WithTimeout(ctx, controlSyncBudget)
	defer cancel()
	if !controlImagesPresent(sctx, cli) {
		return false
	}
	// The child or a control update holds the lock: it owns the pod for now.
	ran, err := recreateControl(sctx, boot, tryControlLock, false, func(ctx context.Context) error {
		return ensureControlPod(ctx, cli)
	})
	if err == nil {
		return true // done, or another process holds the lock and owns the pod
	}
	if !ran {
		failControl(w, boot, err) // couldn't take the lock
		return true
	}
	if sctx.Err() != nil && ctx.Err() == nil {
		return false
	}
	failControl(w, boot, err)
	return true
}

// failControl records a failed attempt for boot and warns once per boot.
func failControl(w io.Writer, boot string, err error) {
	st := readControlState()
	st.AttemptBoot, st.LastAttempt = boot, controlNow()
	st.LastError, st.Conflict = controlFailureClass(err), controlConflict(err)
	if st.WarnedBoot != boot {
		warnControl(w, st)
		st.WarnedBoot = boot
	}
	writeControlState(st)
}

// controlFailureClass buckets an EnsureControlPod error for controlReason.
func controlFailureClass(err error) string {
	switch {
	case errors.Is(err, pod.ErrControlNameTaken):
		return "name-taken"
	case errors.Is(err, pod.ErrControlUnavailable):
		return "unavailable"
	case errors.Is(err, pod.ErrControlDownload):
		return "download"
	case errors.Is(err, pod.ErrControlStart):
		return "start"
	default:
		return "other"
	}
}

// controlReason is the plain-words reason shown to the user. Raw errors are
// never echoed: lower layers speak in runtime internals.
func controlReason(class string) string {
	switch class {
	case "download":
		return "couldn't download it — are you offline?"
	case "unavailable":
		return "it isn't available yet"
	case "start":
		return "it didn't start"
	default:
		return "unexpected error"
	}
}

// controlConflict is the folder of the user's own control project, if err is
// that conflict.
func controlConflict(err error) string {
	var taken *pod.ControlNameTakenError
	if errors.As(err, &taken) {
		return taken.Path
	}
	return ""
}

// controlRenameMessage tells the user how to free the name control.
func controlRenameMessage(path string) string {
	return fmt.Sprintf("your project \"control\" in %s has the name tainer needs for its dashboard — rename it (change project.name in its tainer.yaml) and tainer will set up my.tainer.me", path)
}

func warnControl(w io.Writer, st controlState) {
	if st.LastError == "name-taken" {
		fmt.Fprintf(w, "tainer: %s\n", controlRenameMessage(st.Conflict))
		return
	}
	fmt.Fprintf(w, "tainer: couldn't start the control dashboard yet (%s); it will retry next time tainer starts\n", controlReason(st.LastError))
}

// recordControlFailure stores a failed attempt (used by the child). With an
// unknown boot only the failure class is recorded: the parent's AttemptBoot
// and LastAttempt stay, so the retry throttle keeps working.
func recordControlFailure(boot string, err error) {
	st := readControlState()
	if boot != "" {
		st.AttemptBoot, st.LastAttempt = boot, controlNow()
	}
	st.LastError, st.Conflict = controlFailureClass(err), controlConflict(err)
	writeControlState(st)
}

// launchControlEnsureChild starts a detached `tainer control ensure` that
// outlives this command. Its output goes to <BaseDir>/control-ensure.log.
func launchControlEnsureChild(boot string) error {
	return launchDetached([]string{"control", "ensure"}, "control-ensure.log", []string{controlBootEnv + "=" + boot})
}

// launchDetached starts `tainer <args>` in its own session, so it outlives
// this command, its terminal and its process group (the dashboard's gateway
// ends the whole group of a command whose caller went away). Output goes to
// <BaseDir>/<logName>.
func launchDetached(args []string, logName string, env []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(config.BaseDir(), logName), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = detachedProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// cmdControl handles `tainer control …`, which stays out of the main help.
// `ensure` and `update` are internal (spawned by the runtime-ready hook and
// by `tainer update`); `dev` manages
// dashboard checkouts approved for the control pod's access (documented in
// the tainer-control repo).
func cmdControl(args []string) {
	if len(args) == 1 && args[0] == "ensure" {
		os.Exit(runControlEnsure())
	}
	if len(args) == 1 && args[0] == "update" {
		os.Exit(runControlUpdate())
	}
	if len(args) >= 1 && args[0] == "dev" {
		os.Exit(runControlDev(context.Background(), args[1:], os.Stdout, os.Stderr, liveControlDevOps()))
	}
	fmt.Fprintln(os.Stderr, controlDevUsage)
	os.Exit(2)
}

// runControlEnsure is the detached child: one at a time, own budget, result
// recorded in the stamp file for the next command to read.
func runControlEnsure() int {
	runtime.OnReady = nil // this process is the hook's worker; don't recurse
	if err := config.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !controlLockFree() {
		return 0 // another child (or an update) is already on it
	}

	ctx, cancel := context.WithTimeout(context.Background(), controlChildBudget)
	defer cancel()
	cli, err := controlEngine(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		recordControlFailure(os.Getenv(controlBootEnv), err)
		return 1
	}
	defer cli.Close()
	boot := controlBootID(cli)
	if boot == "" {
		boot = os.Getenv(controlBootEnv)
	}
	ran, err := recreateControl(ctx, boot, tryControlLock, false, func(ctx context.Context) error {
		return ensureControlPod(ctx, cli)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		recordControlFailure(boot, err)
		return 1
	}
	_ = ran // false: another child (or an update) took the lock meanwhile
	return 0
}
