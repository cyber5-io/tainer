package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/release"
)

// selfUpdateSupported is false off macOS, where there is no pkg to install.
var selfUpdateSupported = goruntime.GOOS == "darwin"

var errSelfUpdateUnsupported = errors.New("updating tainer itself works on macOS only — download the new version from https://tainer.dev")

// installOps is everything installTainer and installMenu touch outside
// their own logic, so tests can drive them without a network, Installer
// or a runtime.
type installOps struct {
	check    func(ctx context.Context) (release.Latest, bool)
	download func(ctx context.Context, a release.Artifact, dir string) (string, error)
	// quarantine marks the download so Gatekeeper assesses it when
	// Installer opens it. Failure is not fatal.
	quarantine  func(ctx context.Context, path string) error
	verify      func(ctx context.Context, path string) error
	openAndWait func(ctx context.Context, path string) error // open -W
	// installed is the installed version of what is being updated.
	installed func() string
	// installedTainer is the installed tainer, for the menu's minTainer.
	installedTainer func() string
	snapshot        func(ctx context.Context) ([]release.ResumeEntry, error)
	resume          func(ctx context.Context) (resumeOutcome, error)
	now             func() time.Time
	// in is a *bufio.Reader so callers can chain prompts on one reader.
	in  *bufio.Reader
	out io.Writer
	yes bool
}

// installResult is what one self-update did; `--json` emits it as is.
type installResult struct {
	Mode    string          `json:"mode"`
	From    string          `json:"from"`
	To      string          `json:"to"`
	Updated bool            `json:"updated"`
	Resumed []string        `json:"resumed"`
	Failed  []resumeFailure `json:"failed"`
}

func newInstallResult(mode, from, to string) installResult {
	return installResult{Mode: mode, From: from, To: to, Resumed: []string{}, Failed: []resumeFailure{}}
}

// installedTainerVersion is the installed binary's version, or this CLI's
// when that can't be read.
func installedTainerVersion() string {
	if v := installedBinaryVersion(); v != "" {
		return v
	}
	return version
}

// liveInstallOps wires installOps to the release endpoint, the package
// checks, Installer and the runtime. installed is set per target.
func liveInstallOps(yes bool) installOps {
	return installOps{
		check:    func(ctx context.Context) (release.Latest, bool) { return release.Check(ctx, true) },
		download: release.Download,
		quarantine: func(ctx context.Context, path string) error {
			attr := fmt.Sprintf("0081;%x;tainer;", time.Now().Unix())
			_, err := release.ExecRunner(ctx, "xattr", "-w", "com.apple.quarantine", attr, path)
			return err
		},
		verify: func(ctx context.Context, path string) error {
			return release.VerifyPkg(ctx, release.ExecRunner, path)
		},
		openAndWait: func(ctx context.Context, path string) error {
			return exec.CommandContext(ctx, "open", "-W", path).Run()
		},
		installed:       installedTainerVersion,
		installedTainer: installedTainerVersion,
		snapshot: func(ctx context.Context) ([]release.ResumeEntry, error) {
			return snapshotRunning(ctx, liveResumeOps())
		},
		// Only this caller watched the installer close.
		resume: func(ctx context.Context) (resumeOutcome, error) {
			return runResumeReport(ctx, liveResumeOps(), true)
		},
		now: time.Now,
		in:  bufio.NewReader(os.Stdin),
		out: os.Stdout,
		yes: yes,
	}
}

// liveMenuInstallOps is liveInstallOps for the menu app.
func liveMenuInstallOps(yes bool) installOps {
	ops := liveInstallOps(yes)
	ops.installed = release.InstalledMenu
	return ops
}

// installTainer updates tainer from a verified package: it warns before
// restarting running pods, records them, runs Installer and brings them
// back afterwards.
func installTainer(ctx context.Context, ops installOps) (installResult, error) {
	if !selfUpdateSupported {
		return installResult{}, errSelfUpdateUnsupported
	}
	latest, ok := ops.check(ctx)
	if !ok {
		return installResult{}, errors.New("couldn't find out the latest tainer version — try again later")
	}
	from, to := ops.installed(), latest.Tainer.Version
	res := newInstallResult("tainer", from, to)
	if !release.Newer(to, from) {
		fmt.Fprintln(ops.out, "tainer is up to date")
		return res, nil
	}
	pods, err := ops.snapshot(ctx)
	if err != nil {
		return res, err
	}
	if len(pods) > 0 {
		names := make([]string, len(pods))
		for i, p := range pods {
			names[i] = p.Name
		}
		fmt.Fprintf(ops.out, "Updating tainer restarts your running pods (%s).\n", strings.Join(names, ", "))
		if !ops.yes && !confirm(ops.in, ops.out, "Update now?") {
			fmt.Fprintln(ops.out, "update postponed")
			return res, nil
		}
	}
	err = downloadAndOpen(ctx, ops, latest.Tainer, func() error {
		return release.WriteResume(release.ResumeFile{
			ForVersion:  to,
			FromVersion: from,
			WrittenAt:   ops.now(),
			Pods:        pods,
		})
	})
	if errors.Is(err, errNotOpened) {
		return res, errors.Unwrap(err)
	}
	if ctx.Err() != nil {
		// Interrupted while Installer may still be open: a resume now
		// would fail every start and drop the file. Leave it for
		// postinstall, the menu app or the stale rule.
		return res, errUpdateInterrupted
	}
	// Installer ran (or failed to start): resume either way, so the file
	// is consumed and anything postinstall stopped comes back.
	outcome, rerr := ops.resume(ctx)
	res.Resumed = append(res.Resumed, outcome.Resumed...)
	res.Failed = append(res.Failed, outcome.Failed...)
	res.Updated = sameVersion(ops.installed(), to)
	return res, errors.Join(err, rerr)
}

// installMenu updates the menu app from a verified package. It touches no
// pods.
func installMenu(ctx context.Context, ops installOps) (installResult, error) {
	if !selfUpdateSupported {
		return installResult{}, errSelfUpdateUnsupported
	}
	latest, ok := ops.check(ctx)
	if !ok {
		return installResult{}, errors.New("couldn't find out the latest menu app version — try again later")
	}
	from, to := ops.installed(), latest.Menu.Version
	res := newInstallResult("menu", from, to)
	// An empty installed version means the menu app is missing: install it.
	if to == "" || (from != "" && !release.Newer(to, from)) {
		fmt.Fprintln(ops.out, "the menu app is up to date")
		return res, nil
	}
	if min := latest.Menu.MinTainer; min != "" && release.Newer(min, ops.installedTainer()) {
		return res, fmt.Errorf("the menu app needs tainer %s first", min)
	}
	err := downloadAndOpen(ctx, ops, latest.Menu, nil)
	if errors.Is(err, errNotOpened) {
		return res, errors.Unwrap(err)
	}
	res.Updated = sameVersion(ops.installed(), to)
	return res, err
}

var errUpdateInterrupted = errors.New("the update was interrupted; your pods come back when the install finishes")

// errNotOpened wraps failures before Installer was opened: nothing was
// recorded, so there is nothing to resume.
var errNotOpened = errors.New("not opened")

type notOpenedError struct{ err error }

func (e notOpenedError) Error() string        { return e.err.Error() }
func (e notOpenedError) Unwrap() error        { return e.err }
func (e notOpenedError) Is(target error) bool { return target == errNotOpened }

// downloadAndOpen downloads a into a fresh private temp dir, marks and
// verifies it, runs beforeOpen, then opens it in Installer and waits. The
// temp dir is removed afterwards. Failures before opening come back as
// notOpenedError.
func downloadAndOpen(ctx context.Context, ops installOps, a release.Artifact, beforeOpen func() error) error {
	dir, err := os.MkdirTemp("", "tainer-update-")
	if err != nil {
		return notOpenedError{err}
	}
	defer os.RemoveAll(dir)
	pkg, err := ops.download(ctx, a, dir)
	if err != nil {
		return notOpenedError{err}
	}
	// Best effort: without it Installer still runs, just without a
	// fresh Gatekeeper assessment.
	_ = ops.quarantine(ctx, pkg)
	if err := ops.verify(ctx, pkg); err != nil {
		os.Remove(pkg)
		return notOpenedError{err}
	}
	if beforeOpen != nil {
		if err := beforeOpen(); err != nil {
			return notOpenedError{err}
		}
	}
	if err := ops.openAndWait(ctx, pkg); err != nil {
		return fmt.Errorf("couldn't open the installer: %w", err)
	}
	return nil
}
