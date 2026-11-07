package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// runUpdateCheck reports which registered pods have newer parts available
// without pulling anything. It exits 0 even when updates exist.
func runUpdateCheck(outMode tui.OutputMode) {
	started := time.Now()
	ctx, cancel := ctxWithTimeout(2 * time.Minute)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	if err != nil {
		must(engineErr(err))
		return
	}
	defer eng.Close()

	cd := controlCheckDeps()
	deps := imagecheck.Deps{
		EnginePods:   func(ctx context.Context) ([]pod.Pod, error) { return pod.List(ctx, eng) },
		ImageInspect: func(ctx context.Context, ref string) (engine.ImageInfo, error) { return eng.ImageInspect(ctx, ref) },
		RemoteConfigDigest: func(ctx context.Context, ref imagecheck.Ref, arch string) (string, error) {
			return imagecheck.RemoteConfigDigest(ctx, nil, ref, arch)
		},
		RemoteLabels:  cd.RemoteLabels,
		TainerVersion: cd.TainerVersion,
	}
	// The control pod is checked like any pod (against its stable channel).
	rep, err := imagecheck.Run(ctx, deps, registry.All())
	if err != nil {
		must(engineErr(err))
		return
	}

	switch outMode {
	case tui.ModeJSON:
		jsonDoc(rep)
	case tui.ModePlain:
		for _, p := range rep.Pods {
			if p.UpdateAvailable {
				fmt.Printf("update available: %s (%s)\n", p.Name, p.Parts)
			}
		}
		for _, p := range heldPods(rep) {
			fmt.Printf("needs a newer tainer: %s (%s)\n", p.Name, heldDetail(p))
		}
		for _, p := range unknownPods(rep) {
			fmt.Printf("couldn't check: %s - %s\n", p.Name, p.Error)
		}
		fmt.Println(updateCheckSummary(rep))
	default:
		tui.Bookend("update", "check")
		for _, p := range rep.Pods {
			if p.UpdateAvailable {
				fmt.Println("  " + tui.MarkInfo() + p.Name + "  " + p.Parts)
			}
		}
		for _, p := range heldPods(rep) {
			fmt.Println("  " + tui.MarkWarn() + p.Name + "  needs a newer tainer — " + heldDetail(p))
		}
		for _, p := range unknownPods(rep) {
			fmt.Println("  " + tui.MarkWarn() + "couldn't check: " + p.Name + " — " + p.Error)
		}
		if rep.Available > 0 || rep.Unknown > 0 || rep.Held > 0 {
			fmt.Println()
		}
		tui.BookendClose(time.Since(started), updateCheckSummary(rep))
	}
}

// unknownPods are the pods counted as unknown: a check failed and no role
// shows an update.
func unknownPods(r imagecheck.Report) []imagecheck.PodReport {
	var out []imagecheck.PodReport
	for _, p := range r.Pods {
		if p.Error != "" && !p.UpdateAvailable {
			out = append(out, p)
		}
	}
	return out
}

// heldPods are the pods whose newer image needs a newer tainer.
func heldPods(r imagecheck.Report) []imagecheck.PodReport {
	var out []imagecheck.PodReport
	for _, p := range r.Pods {
		if p.Control != nil && p.Control.State == imagecheck.ControlNeedsNewerTainer {
			out = append(out, p)
		}
	}
	return out
}

// heldDetail names the held image and the tainer it needs.
func heldDetail(p imagecheck.PodReport) string {
	return fmt.Sprintf("dashboard %s needs tainer %s or later", p.Control.Stable, p.Control.MinTainer)
}

func updateCheckSummary(r imagecheck.Report) string {
	s := fmt.Sprintf("%d up to date · %d updates available · %d unknown", r.UpToDate, r.Available, r.Unknown)
	if r.Held > 0 {
		s += fmt.Sprintf(" · %d needs a newer tainer", r.Held)
	}
	return s
}

// updateCheckUsageErr is returned when --check is combined with another mode.
func updateCheckUsageErr() error {
	return withCode("invalid-arguments", fmt.Errorf("--check cannot be combined with --all, --base or a project name"))
}
