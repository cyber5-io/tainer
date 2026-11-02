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

	deps := imagecheck.Deps{
		EnginePods:   func(ctx context.Context) ([]pod.Pod, error) { return pod.List(ctx, eng) },
		ImageInspect: func(ctx context.Context, ref string) (engine.ImageInfo, error) { return eng.ImageInspect(ctx, ref) },
		RemoteConfigDigest: func(ctx context.Context, ref imagecheck.Ref, arch string) (string, error) {
			return imagecheck.RemoteConfigDigest(ctx, nil, ref, arch)
		},
	}
	rep, err := imagecheck.Run(ctx, deps, visibleProjects(registry.All()))
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
		for _, p := range unknownPods(rep) {
			fmt.Println("  " + tui.MarkWarn() + "couldn't check: " + p.Name + " — " + p.Error)
		}
		if rep.Available > 0 || rep.Unknown > 0 {
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

func updateCheckSummary(r imagecheck.Report) string {
	return fmt.Sprintf("%d up to date · %d updates available · %d unknown", r.UpToDate, r.Available, r.Unknown)
}

// updateCheckUsageErr is returned when --check is combined with another mode.
func updateCheckUsageErr() error {
	return withCode("invalid-arguments", fmt.Errorf("--check cannot be combined with --all, --base or a project name"))
}
