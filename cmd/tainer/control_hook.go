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
//   - control images present locally → EnsureControlPod runs in-process
//     under a short bound that never eats into the caller's deadline;
//   - images missing (first run, an update) or too little caller budget →
//     a detached hidden `tainer control ensure` child does it on its own
//     budget and the caller returns immediately.
//
// Failed attempts are retried at most every controlRetryEvery per boot, and
// the one-line warning is printed at most once per boot.
const (
	controlRetryEvery     = 10 * time.Minute
	controlDeadlineMargin = 10 * time.Second
	controlMinSync        = 3 * time.Second
	controlChildBudget    = 5 * time.Minute
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
	if err := launchControlEnsure(); err != nil {
		failControl(w, boot, err)
	}
}

// ensureInProcess runs EnsureControlPod synchronously when the images are
// local and the caller has budget to spare. It returns false when the work
// should go to the detached child instead: images missing, too little
// budget, or only our own short bound ran out (a slow cold start).
func ensureInProcess(ctx context.Context, cli *engine.Client, boot string, w io.Writer) bool {
	budget := controlSyncWindow(ctx)
	if budget <= 0 {
		return false
	}
	sctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if !controlImagesPresent(sctx, cli) {
		return false
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

// controlSyncWindow is how long the in-process ensure may run: at most
// controlSyncBudget and never into the caller's last controlDeadlineMargin.
// Zero means: no synchronous work, hand off to the child.
func controlSyncWindow(ctx context.Context) time.Duration {
	b := controlSyncBudget
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl) - controlDeadlineMargin; left < b {
			b = left
		}
		if b < controlMinSync && b < controlSyncBudget {
			return 0
		}
	}
	return b
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

// recordControlFailure stores a failed attempt for boot (used by the child).
func recordControlFailure(boot string, err error) {
	st := readControlState()
	st.AttemptBoot, st.LastAttempt, st.LastError = boot, controlNow(), controlFailureClass(err)
	writeControlState(st)
}

// launchControlEnsureChild starts a detached `tainer control ensure` that
// outlives this command. Its output goes to <BaseDir>/control-ensure.log.
func launchControlEnsureChild() error {
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
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = detachedProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// cmdControl handles `tainer control …`. `ensure` is internal (spawned by
// the runtime-ready hook) and deliberately absent from help.
func cmdControl(args []string) {
	if len(args) == 1 && args[0] == "ensure" {
		os.Exit(runControlEnsure())
	}
	usage()
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
	release, ok, err := tryLock(filepath.Join(config.BaseDir(), "control-ensure.lock"))
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
		recordControlFailure("", err)
		return 1
	}
	defer cli.Close()
	boot := controlBootID(cli)
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
