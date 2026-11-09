package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/release"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// resumeStaleAfter is how old a resume file for a version that never got
// installed must be before a resumer that didn't watch the installer
// treats it as left by a cancelled install, and drops it.
const resumeStaleAfter = time.Hour

// errRuntimeUnreachable marks a list failure caused by the runtime not
// answering; when snapshotting, that means nothing is running.
var errRuntimeUnreachable = errors.New("tainer isn't running")

// resumeUnreadableMsg is printed when the resume file can't be read.
const resumeUnreadableMsg = "couldn't read the list of pods to bring back; start them with tainer start"

// installedTainerBin is the installed tainer binary. The pkg replaces it
// before postinstall runs, so its version is right immediately — unlike
// the pkg receipt, which macOS writes only after postinstall.
var installedTainerBin = "/opt/tainer/bin/tainer"

// installedVersionTimeout bounds `tainer version` on the installed binary.
var installedVersionTimeout = 5 * time.Second

// installedBinaryVersion runs `<installedTainerBin> version` and returns
// its trimmed output, or "" on any failure.
func installedBinaryVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), installedVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, installedTainerBin, "version")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resumeOps is everything runResume and snapshotRunning touch outside
// the resume file, so tests can drive them without a runtime.
type resumeOps struct {
	list      func(ctx context.Context) ([]release.ResumeEntry, error)
	start     func(ctx context.Context, e release.ResumeEntry) error
	installed func() string
	now       func() time.Time
	out       io.Writer
}

// resumeOpsLive is the resumeOps the installers use; a seam for tests.
var resumeOpsLive = liveResumeOps

// liveResumeOps wires resumeOps to the real runtime, registry and the
// installed tainer binary.
func liveResumeOps() resumeOps {
	return resumeOps{
		list: func(ctx context.Context) ([]release.ResumeEntry, error) {
			eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
			if err != nil {
				// Without AutoStart, every Engine failure is "not answering".
				return nil, fmt.Errorf("%w: %v", errRuntimeUnreachable, err)
			}
			defer eng.Close()
			pods, err := pod.List(ctx, eng)
			if err != nil {
				return nil, err
			}
			return runningEntries(pods, registry.All()), nil
		},
		start: func(ctx context.Context, e release.ResumeEntry) error {
			// AutoStart brings up the freshly installed runtime when the
			// installer stopped the old one.
			eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
			if err != nil {
				return engineErr(err)
			}
			defer eng.Close()
			// An already-running pod comes back as AlreadyRunning, not an error.
			_, err = pod.Start(ctx, eng, pod.StartOptions{
				ManifestPath: filepath.Join(e.Path, manifest.FileName),
				ProjectDir:   e.Path,
			})
			return err
		},
		installed: installedBinaryVersion,
		now:       time.Now,
		out:       os.Stdout,
	}
}

// runningEntries joins running or partly running pods with the registry.
// Pods with no registered folder are skipped: there is nothing to start
// them from.
func runningEntries(pods []pod.Pod, projects map[string]registry.Project) []release.ResumeEntry {
	var out []release.ResumeEntry
	for _, p := range pods {
		if s := p.State(); s != pod.StateRunning && s != pod.StateMixed {
			continue
		}
		proj, ok := projects[p.Name]
		if !ok || proj.Path == "" {
			continue
		}
		out = append(out, release.ResumeEntry{Name: p.Name, Path: proj.Path})
	}
	return out
}

// snapshotRunning lists the pods to bring back after an update. The
// control pod is left out: it returns on its own when the runtime boots.
// A runtime that doesn't answer has nothing running: empty list, no error.
func snapshotRunning(ctx context.Context, ops resumeOps) ([]release.ResumeEntry, error) {
	entries, err := ops.list(ctx)
	if errors.Is(err, errRuntimeUnreachable) {
		return []release.ResumeEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]release.ResumeEntry, 0, len(entries))
	for _, e := range entries {
		if tainer.IsPinnedProject(e.Name) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// runResume brings back the pods recorded before an update, once the
// outcome of the install is known. installerDone is true only when the
// caller watched the installer close; then the recorded pods are started
// whatever the outcome. Every other caller passes false: it starts them
// once the new version is installed, leaves the file while the installer
// may still be open, and drops a stale file without starting anything
// (the install was cancelled, so nothing was stopped).
func runResume(ctx context.Context, ops resumeOps, installerDone bool) error {
	_, err := runResumeReport(ctx, ops, installerDone)
	return err
}

// resumeFailure is a pod that didn't come back, and why.
type resumeFailure struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// resumeOutcome is what a resume started and what it couldn't.
type resumeOutcome struct {
	Resumed []string
	Failed  []resumeFailure
}

// runResumeReport is runResume that also returns which pods came back
// and which didn't; the printed progress is the same.
func runResumeReport(ctx context.Context, ops resumeOps, installerDone bool) (resumeOutcome, error) {
	var res resumeOutcome
	err := release.WithResumeLock(func() error {
		f, ok, err := release.ReadResume()
		if err != nil {
			// Nothing safe to act on: drop it and point at tainer start.
			fmt.Fprintln(ops.out, resumeUnreadableMsg)
			return release.ClearResume()
		}
		if !ok {
			return nil
		}
		installed := ops.installed()
		switch {
		case sameVersion(installed, f.ForVersion):
			// The install went through.
		case installerDone:
			// Cancelled or failed; the old runtime may already be stopped.
		case ops.now().Sub(f.WrittenAt) > resumeStaleAfter:
			// Left by a cancelled install: discard it, don't act on it.
			return release.ClearResume()
		default:
			// The installer may still be open; leave it for later.
			return nil
		}
		for _, e := range f.Pods {
			if err := ops.start(ctx, e); err != nil {
				reason := resumeReason(e.Name, err)
				fmt.Fprintf(ops.out, "%s didn't come back: %s\n", e.Name, reason)
				res.Failed = append(res.Failed, resumeFailure{Name: e.Name, Reason: reason})
				continue
			}
			fmt.Fprintf(ops.out, "%s is back\n", e.Name)
			res.Resumed = append(res.Resumed, e.Name)
		}
		return release.ClearResume()
	})
	return res, err
}

// sameVersion compares versions ignoring a leading "v"; an empty
// installed version never matches.
func sameVersion(installed, want string) bool {
	installed = strings.TrimPrefix(installed, "v")
	return installed != "" && installed == strings.TrimPrefix(want, "v")
}

// runUpdateResume is `tainer update resume` (hidden). Pods that fail to
// come back are reported by name; the command still exits 0.
func runUpdateResume() {
	ctx, cancel := ctxWithTimeout(15 * time.Minute)
	defer cancel()
	must(runResume(ctx, liveResumeOps(), false))
}

// resumeReason is the plain-words reason a pod didn't come back. Raw
// errors are never echoed: lower layers speak in runtime internals.
// Errors whose message is already written for users pass through.
func resumeReason(name string, err error) string {
	var busy *pod.BusyRemovalError
	var dl *engine.DownloadError
	switch {
	case errors.As(err, &busy):
		return busy.Error()
	case errors.As(err, &dl):
		return dl.Error()
	case errors.Is(err, fs.ErrNotExist):
		return "its folder or tainer.yaml is gone"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return fmt.Sprintf(`it took too long to start — run "tainer start %s"`, name)
	}
	switch errCode(err) {
	case "stack-unreachable", "stack-unavailable":
		return fmt.Sprintf(`tainer isn't answering — run "tainer start %s" in a minute`, name)
	}
	return fmt.Sprintf(`it didn't start — run "tainer start %s" to see why`, name)
}
