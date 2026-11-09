package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// updateCheckDoc is `update --check --json`: the pod report with tainer
// and the menu app beside it. New keys are additive; the pod keys keep
// their shape.
type updateCheckDoc struct {
	imagecheck.Report
	Tainer tainerRelease `json:"tainer"`
	Menu   menuRelease   `json:"menu"`
	// ReleaseCheck is "unavailable" when latest.json couldn't be fetched
	// and nothing is cached.
	ReleaseCheck string `json:"releaseCheck"`
	// PodsCheck is "unavailable" when tainer isn't running; pods is then [].
	PodsCheck string `json:"podsCheck"`
}

// buildUpdateCheck checks tainer and the menu app first (a fresh fetch),
// then the pods. Neither failing stops the other.
func buildUpdateCheck(ctx context.Context) updateCheckDoc {
	st := checkSelf(ctx, true)
	doc := updateCheckDoc{Tainer: st.Tainer, Menu: st.Menu, ReleaseCheck: "ok", PodsCheck: "ok"}
	if !st.ok {
		doc.ReleaseCheck = "unavailable"
	}
	// The control pod is checked like any pod (against its stable channel).
	rep, err := podCheck(ctx, false, registry.All())
	if err != nil {
		doc.PodsCheck = "unavailable"
		rep = imagecheck.Report{CheckedAt: jsonTime(time.Now())}
	}
	if rep.Pods == nil {
		rep.Pods = []imagecheck.PodReport{}
	}
	doc.Report = rep
	return doc
}

// runUpdateCheck reports what has an update — tainer, the menu app and
// each registered pod — without changing anything. It exits 0 even when
// updates exist or tainer isn't running.
func runUpdateCheck(outMode tui.OutputMode) {
	started := time.Now()
	ctx, cancel := ctxWithTimeout(2 * time.Minute)
	defer cancel()

	doc := buildUpdateCheck(ctx)
	rep := doc.Report

	switch outMode {
	case tui.ModeJSON:
		jsonDoc(doc)
	case tui.ModePlain:
		if doc.Tainer.UpdateAvailable {
			fmt.Println("update available: " + tainerLine(doc.Tainer))
		}
		if doc.Menu.UpdateAvailable {
			fmt.Println("update available: " + menuLine(doc.Menu))
		}
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
		if doc.PodsCheck != "ok" {
			fmt.Println("pods weren't checked - tainer isn't running")
		}
		fmt.Println(updateCheckSummary(rep))
	default:
		tui.Bookend("update", "check")
		shown := false
		if doc.Tainer.UpdateAvailable {
			fmt.Println("  " + tui.MarkInfo() + tainerLine(doc.Tainer))
			shown = true
		}
		if doc.Menu.UpdateAvailable {
			fmt.Println("  " + tui.MarkInfo() + menuLine(doc.Menu))
			shown = true
		}
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
		if doc.PodsCheck != "ok" {
			fmt.Println("  " + tui.MarkWarn() + "pods weren't checked — tainer isn't running")
			shown = true
		}
		if shown || rep.Available > 0 || rep.Unknown > 0 || rep.Held > 0 {
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
