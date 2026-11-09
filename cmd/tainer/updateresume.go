package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/release"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// resumeStaleAfter is how old a resume file must be before a resumer that
// cannot tell whether an install is still open treats it as abandoned.
const resumeStaleAfter = time.Hour

// errRuntimeUnreachable marks a list failure caused by the runtime not
// answering; when snapshotting, that means nothing is running.
var errRuntimeUnreachable = errors.New("tainer isn't running")

// resumeUnreadableMsg is printed when the resume file can't be read.
const resumeUnreadableMsg = "couldn't read the list of pods to bring back; start them with tainer start"

// resumeOps is everything runResume and snapshotRunning touch outside
// the resume file, so tests can drive them without a runtime.
type resumeOps struct {
	list      func(ctx context.Context) ([]release.ResumeEntry, error)
	start     func(ctx context.Context, e release.ResumeEntry) error
	installed func() string
	now       func() time.Time
	out       io.Writer
}

// liveResumeOps wires resumeOps to the real runtime, registry and pkg
// receipt.
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
		installed: func() string { return release.InstalledTainer(release.ExecRunner) },
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
// caller watched the installer close; every other caller passes false and
// waits for the install to finish, or for the file to go stale.
func runResume(ctx context.Context, ops resumeOps, installerDone bool) error {
	return release.WithResumeLock(func() error {
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
			// Left behind by a crash or an abandoned install.
		default:
			// The installer may still be open; leave it for later.
			return nil
		}
		for _, e := range f.Pods {
			if err := ops.start(ctx, e); err != nil {
				fmt.Fprintf(ops.out, "%s didn't come back: %v\n", e.Name, err)
				continue
			}
			fmt.Fprintf(ops.out, "%s is back\n", e.Name)
		}
		return release.ClearResume()
	})
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
