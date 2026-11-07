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
	WarnedBoot  string    `json:"warnedBoot,omitempty"`
}

// controlLockPath serialises everything that recreates the control pod: the
// ensure (in-process or the detached child) and a control update.
func controlLockPath() string { return filepath.Join(config.BaseDir(), "control-ensure.lock") }

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
	now := controlNow()
	if st.AttemptBoot == boot && now.Sub(st.LastAttempt) < controlRetryEvery {
		// An attempt for this boot is running in a child or failed recently.
		if st.LastError != "" && st.WarnedBoot != boot {
			warnControl(w, st.LastError)
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
	release, ok, lerr := tryLock(controlLockPath())
	if lerr == nil && !ok {
		return true
	}
	if lerr == nil {
		defer release()
	}
	err := ensureControlPod(sctx, cli)
	if err == nil {
		st := readControlState()
		st.EnsuredBoot, st.LastError = boot, ""
		writeControlState(st)
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
	st.AttemptBoot, st.LastAttempt, st.LastError = boot, controlNow(), controlFailureClass(err)
	if st.WarnedBoot != boot {
		warnControl(w, st.LastError)
		st.WarnedBoot = boot
	}
	writeControlState(st)
}

// controlFailureClass buckets an EnsureControlPod error for controlReason.
func controlFailureClass(err error) string {
	switch {
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
	case "start":
		return "it didn't start"
	default:
		return "unexpected error"
	}
}

func warnControl(w io.Writer, class string) {
	fmt.Fprintf(w, "tainer: couldn't start the control dashboard yet (%s); it will retry next time tainer starts\n", controlReason(class))
}

// recordControlFailure stores a failed attempt (used by the child). With an
// unknown boot only the failure class is recorded: the parent's AttemptBoot
// and LastAttempt stay, so the retry throttle keeps working.
func recordControlFailure(boot string, err error) {
	st := readControlState()
	if boot != "" {
		st.AttemptBoot, st.LastAttempt = boot, controlNow()
	}
	st.LastError = controlFailureClass(err)
	writeControlState(st)
}

// launchControlEnsureChild starts a detached `tainer control ensure` that
// outlives this command. Its output goes to <BaseDir>/control-ensure.log.
func launchControlEnsureChild(boot string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(config.BaseDir(), "control-ensure.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(exe, "control", "ensure")
	cmd.Env = append(os.Environ(), controlBootEnv+"="+boot)
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
// `ensure` is internal (spawned by the runtime-ready hook); `dev` manages
// dashboard checkouts approved for the control pod's access (documented in
// the tainer-control repo).
func cmdControl(args []string) {
	if len(args) == 1 && args[0] == "ensure" {
		os.Exit(runControlEnsure())
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
	release, ok, err := tryLock(controlLockPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !ok {
		return 0 // another child is already on it
	}
	defer release()

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
	if err := ensureControlPod(ctx, cli); err != nil {
		fmt.Fprintln(os.Stderr, err)
		recordControlFailure(boot, err)
		return 1
	}
	st := readControlState()
	st.EnsuredBoot, st.LastError = boot, ""
	writeControlState(st)
	return 0
}
