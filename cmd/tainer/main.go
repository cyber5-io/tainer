// Package main is the entry point for the tainer CLI — the user-facing
// tool that drives a CyberStack engine to run local development
// containers.
//
// Commands wired here:
//
//	tainer version                           print the linker-stamped version
//	tainer status                            probe cyberstackd, list pods
//	tainer init                              scaffold wizard (Task 24 placeholder)
//	tainer start [project]                   bring a pod up
//	tainer stop [project]                    stop a pod
//	tainer destroy [project] [--clean|--nuke] tear down a pod
//	tainer exec <project> [<role>] -- <cmd>  run a command in a container
//	tainer list (ls)                         list all pods
//	tainer db export|import <project> ...    dump / restore database
//	tainer update [--base <type>|--all|project] refresh images
//	tainer network mode show|set <mode>      inspect / switch network mode
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"

	"github.com/cyber5-io/tainer/pkg/tainer/doctor"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/initcmd"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/networkcmd"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
	tuilist "github.com/cyber5-io/tainer/pkg/tainer/tui/list"
)

// version is stamped at link time; see Makefile.
var version = "dev"

func main() {
	tui.SetVersion(version)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "ui-demo":
		// Hidden command — visual sandbox for the brand surface.
		// Not in usage() on purpose; documented in pkg/tainer/tui/.
		cmdUIDemo(os.Args[2:])
	case "status":
		cmdStatus()
	case "doctor":
		cmdDoctor(os.Args[2:])
	case "init":
		cmdInit(os.Args[2:])
	case "start":
		cmdStart(os.Args[2:])
	case "stop":
		cmdStop(os.Args[2:])
	case "destroy":
		cmdDestroy(os.Args[2:])
	case "exec":
		cmdExec(os.Args[2:])
	case "wp", "artisan", "npm", "yarn", "pnpm", "composer", "node", "php":
		cmdExecWrapper(os.Args[1], os.Args[2:])
	case "list", "ls":
		cmdList(os.Args[2:])
	case "db":
		cmdDB(os.Args[2:])
	case "update":
		cmdUpdate(os.Args[2:])
	case "network":
		cmdNetwork(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "tainer "+version)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "usage: tainer <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  init <type> [name]                     scaffold a new project into cwd")
	fmt.Fprintln(os.Stderr, "  start [project]                        start a project")
	fmt.Fprintln(os.Stderr, "  stop [project]                         stop a project")
	fmt.Fprintln(os.Stderr, "  destroy [project] [--clean|--nuke]     tear down a project")
	fmt.Fprintln(os.Stderr, "  exec <project> [<role>] -- <cmd...>    run a command in a container")
	fmt.Fprintln(os.Stderr, "  wp|composer|artisan|php <args...>      run the tool in the current project")
	fmt.Fprintln(os.Stderr, "  npm|yarn|pnpm|node <args...>           run the tool in the current project")
	fmt.Fprintln(os.Stderr, "  status                                 show pod state(s)")
	fmt.Fprintln(os.Stderr, "  doctor [--fix]                         check every layer of the stack")
	fmt.Fprintln(os.Stderr, "  list (ls)                              list all pods")
	fmt.Fprintln(os.Stderr, "  db export <project> [outfile]          dump database")
	fmt.Fprintln(os.Stderr, "  db import <project> <file>             restore database")
	fmt.Fprintln(os.Stderr, "  update [project|--base <type>|--all]   refresh image(s)")
	fmt.Fprintln(os.Stderr, "  network mode show|set <perf|compat>    inspect / switch network mode")
	fmt.Fprintln(os.Stderr, "  version                                print version")
}

// must prints err to stderr and exits 1. No-op when err is nil.
func must(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "tainer:", err)
	os.Exit(1)
}

// ctxWithTimeout is a convenience wrapper around context.WithTimeout.
func ctxWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// locateManifest resolves a project's tainer.yaml from args + cwd.
//
// Two modes by convention applied across every tainer command:
//
//   - args empty: walk up from cwd looking for tainer.yaml. This lets
//     users run `tainer <verb>` from any depth inside the project tree
//     (html/, data/, html/src/components, etc.) without thinking. Fail
//     if we reach the filesystem root.
//
//   - args[0] non-empty: ignore cwd entirely and look the name up in
//     the registry (populated by `tainer init`). Fail if the name isn't
//     registered — no path-guessing fallback.
//
// Returns a typed error so callers can render a brand-styled message
// rather than the raw `manifest.Load` "no such file" that this used to
// surface.
func locateManifest(args []string) (manifestPath, projectDir string, err error) {
	if len(args) > 0 && args[0] != "" {
		name := args[0]
		p, ok := registry.Get(name)
		if !ok || p.Path == "" {
			return "", "", fmt.Errorf("unknown project %q (run `tainer list` to see registered projects)", name)
		}
		projectDir = p.Path
		manifestPath = filepath.Join(projectDir, manifest.FileName)
		return manifestPath, projectDir, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	return findManifestUpward(cwd)
}

// findManifestUpward walks d → parent → parent... looking for
// tainer.yaml. Returns the (manifestPath, projectDir, nil) of the first
// hit. If the search reaches the filesystem root without finding one,
// returns a guiding error rather than letting manifest.Load surface a
// terse "no such file".
func findManifestUpward(d string) (manifestPath, projectDir string, err error) {
	cur := d
	for {
		candidate := filepath.Join(cur, manifest.FileName)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Hit the root without finding a manifest.
			return "", "", fmt.Errorf("no tainer project found at or above %s (run from a project dir, or pass a project name: `tainer <verb> <name>`)", d)
		}
		cur = parent
	}
}

// cmdStatus shows daemon + pod state. Scope is contextual:
//
//	tainer status                   project-detail view if cwd is inside a project,
//	                                otherwise full all-pods view (with daemon block)
//	tainer status <name>            project-detail view for the named project (registry lookup)
//	tainer status --global          force the all-pods view even from a project dir
//
// Project-detail view drills into the individual containers (web, db,
// cron, etc.) with per-container state, IP, role; the all-pods view
// shows a one-line summary per pod, no container details.
//
// Output flags: --plain (auto when piped), --json, --no-color.
// Exits non-zero when the daemon is unreachable.
func cmdStatus() {
	flags, rest := tui.ParseOutputFlags(os.Args[2:])
	mode := flags.Resolve(false)

	// Parse --global out of rest (boolean, anywhere on the command
	// line) so the positional project name still works after.
	var global bool
	posArgs := make([]string, 0, len(rest))
	for _, a := range rest {
		if a == "--global" || a == "-g" {
			global = true
			continue
		}
		posArgs = append(posArgs, a)
	}

	ctx, cancel := ctxWithTimeout(2 * time.Second)
	defer cancel()

	focus := resolveStatusFocus(global, posArgs)
	if focus.errMsg != "" {
		must(fmt.Errorf("%s", focus.errMsg))
	}

	if focus.project != "" {
		// Project-detail view (cwd-resolved OR positional name).
		snap := collectProjectStatus(ctx, focus.project)
		switch mode {
		case tui.ModeJSON:
			runStatusProjectJSON(snap)
		case tui.ModePlain:
			runStatusProjectPlain(snap)
		default:
			runStatusProjectStyled(snap)
		}
		if !snap.Daemon.Reachable {
			os.Exit(1)
		}
		return
	}

	// All-pods view.
	snap := collectStatus(ctx)
	switch mode {
	case tui.ModeJSON:
		runStatusJSON(snap)
	case tui.ModePlain:
		runStatusPlain(snap)
	default:
		runStatusStyled(snap)
	}
	if !snap.Daemon.Reachable {
		os.Exit(1)
	}
}

// statusFocus encodes the resolved scope of a status invocation.
type statusFocus struct {
	project string // non-empty → render project-detail view
	errMsg  string // non-empty → must() this and bail
}

// resolveStatusFocus applies the cwd-walk / registry / --global rules
// and returns either a project name to focus on or "" for all-pods.
func resolveStatusFocus(global bool, posArgs []string) statusFocus {
	if global && len(posArgs) > 0 {
		return statusFocus{errMsg: "--global cannot be combined with a project name"}
	}
	if global {
		return statusFocus{} // all-pods view
	}
	if len(posArgs) > 0 {
		// Named project — must be in the registry. locateManifest does
		// the registry lookup AND the project-name validation in one go.
		// We discard the manifest path; we just need the resolved name.
		if _, _, err := locateManifest(posArgs); err != nil {
			return statusFocus{errMsg: err.Error()}
		}
		return statusFocus{project: posArgs[0]}
	}
	// No args — try cwd walk-up. If we find a manifest, focus on its
	// project. If not, fall through to the all-pods view (intentional:
	// `tainer status` from $HOME shouldn't error, it should give the
	// big-picture view).
	_, _, err := locateManifest(nil)
	if err != nil {
		return statusFocus{} // fall back to all-pods
	}
	// Reload the manifest to get the project name. locateManifest
	// returned us the path; manifest.Load gives us the name.
	mPath, _, _ := locateManifest(nil)
	m, mErr := manifest.Load(mPath)
	if mErr != nil {
		return statusFocus{errMsg: mErr.Error()}
	}
	return statusFocus{project: m.Project.Name}
}

// projectStatusSnapshot is the detail view's data shape.
type projectStatusSnapshot struct {
	Daemon     daemonStatus
	Project    string
	PodID      int
	State      string
	Domain     string
	Ports      []manifest.PortEntry
	Containers []containerStatus
}

type containerStatus struct {
	Role  string `json:"role"`
	Name  string `json:"name"`
	State string `json:"state"`
	IP    string `json:"ip,omitempty"`
}

// collectProjectStatus assembles the project-detail view. Returns a
// snapshot even when the pod isn't running (only Domain/Ports populated
// from the manifest) so the renderer can show "Pod: not running" with
// the expected services from the manifest.
func collectProjectStatus(ctx context.Context, project string) projectStatusSnapshot {
	st := runtime.CurrentStatus(ctx, runtime.Options{})
	snap := projectStatusSnapshot{
		Daemon: daemonStatus{
			Socket:    st.Socket,
			Running:   st.Running,
			PID:       st.PID,
			Reachable: st.Reachable,
		},
		Project: project,
	}

	// Try to load the manifest via the registry to get domain + ports
	// even when the daemon is down or the pod isn't running.
	if mPath, _, err := locateManifest([]string{project}); err == nil {
		if m, mErr := manifest.Load(mPath); mErr == nil {
			snap.Domain = m.Project.Domain
			snap.Ports = m.Ports
		}
	}

	if !st.Reachable {
		return snap
	}
	eng, err := engine.New()
	if err != nil {
		return snap
	}
	defer eng.Close()
	p, err := pod.Get(ctx, eng, project)
	if err != nil || p == nil {
		// Pod doesn't exist yet — manifest data is still populated above.
		return snap
	}
	snap.PodID = p.PodID
	snap.State = string(p.State())
	for _, c := range p.Containers {
		snap.Containers = append(snap.Containers, containerStatus{
			Role:  c.Role,
			Name:  c.Name,
			State: c.State,
			IP:    c.IP,
		})
	}
	return snap
}

func runStatusProjectStyled(s projectStatusSnapshot) {
	tui.Bookend(s.Project, "status")
	muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)

	// Daemon line — short, single line. Don't bury this in a sub-block
	// because the project view is already focused; the daemon health
	// just needs to be visible.
	if s.Daemon.Reachable {
		fmt.Println("  " + tui.MarkSuccess() + "daemon  " + muted.Render(formatDaemonRunningLine(s.Daemon)))
	} else {
		fmt.Println("  " + tui.MarkError() + "daemon  " + muted.Render(formatDaemonRunningLine(s.Daemon)))
		fmt.Println()
		tui.BookendCloseError(0, "Unhealthy", "daemon not reachable — run `tainer start` to bring it up")
		return
	}

	// --- Pod block ---
	fmt.Println()
	fmt.Println("  " + muted.Render("Pod"))
	if s.State == "" {
		fmt.Println("    " + tui.MarkInfo() + s.Project + "  " + muted.Render("not running (no containers)"))
	} else {
		mark := tui.MarkInfo()
		switch s.State {
		case "running":
			mark = tui.MarkSuccess()
		case "mixed":
			mark = tui.MarkWarn()
		}
		bold := lipgloss.NewStyle().Bold(true)
		fmt.Println("    " + mark + bold.Render(s.Project) + "  " + muted.Render(fmt.Sprintf("pod %d · %s", s.PodID, s.State)))
	}

	// --- Containers block (only if we have them) ---
	if len(s.Containers) > 0 {
		fmt.Println()
		fmt.Println("  " + muted.Render(fmt.Sprintf("Containers (%d)", len(s.Containers))))
		nameW, roleW := 4, 4
		for _, c := range s.Containers {
			if l := len(c.Name); l > nameW {
				nameW = l
			}
			if l := len(c.Role); l > roleW {
				roleW = l
			}
		}
		for _, c := range s.Containers {
			mark := tui.MarkInfo()
			switch c.State {
			case "running":
				mark = tui.MarkSuccess()
			case "exited", "stopped", "dead":
				mark = tui.MarkError()
			case "created", "paused", "restarting":
				mark = tui.MarkWarn()
			}
			ip := c.IP
			if ip == "" {
				ip = "-"
			}
			fmt.Printf("    %s%-*s  %-*s  %s  %s\n",
				mark, roleW, c.Role, nameW, c.Name,
				muted.Render(c.State), muted.Render(ip))
		}
	}

	// --- Services block (always when we have manifest data) ---
	if s.Domain != "" {
		fmt.Println()
		fmt.Println("  " + muted.Render("Services"))
		fmt.Println("    " + tui.MarkInfo() + "https://" + s.Domain)
		for _, port := range s.Ports {
			switch port.Protocol {
			case manifest.PortHTTP:
				fmt.Printf("    "+tui.MarkInfo()+"https://%s:%d %s\n", s.Domain, port.Container, muted.Render("("+port.Role+")"))
			case manifest.PortTCP:
				host := pod.DerivePort(s.PodID, port.Role)
				if s.PodID == 0 {
					// Pod not running — port is hypothetical.
					fmt.Printf("    "+tui.MarkInfo()+"<pending> %s\n", muted.Render("("+port.Role+")"))
				} else {
					fmt.Printf("    "+tui.MarkInfo()+"127.0.0.1:%d %s\n", host, muted.Render("("+port.Role+")"))
				}
			}
		}
		fmt.Println("    " + tui.MarkInfo() + "ssh " + s.Project + "@ssh.tainer.me")
	}

	fmt.Println()
	if s.State == "running" {
		runningCt := 0
		for _, c := range s.Containers {
			if c.State == "running" {
				runningCt++
			}
		}
		tui.BookendClose(0, "Healthy", fmt.Sprintf("%d/%d containers running", runningCt, len(s.Containers)))
	} else if s.State == "" {
		tui.BookendClose(0, "Stopped", "run `tainer start` to bring the pod up")
	} else {
		tui.BookendClose(0, s.State)
	}
}

func runStatusProjectPlain(s projectStatusSnapshot) {
	fmt.Printf("socket:    %s\n", s.Daemon.Socket)
	fmt.Printf("running:   %v", s.Daemon.Running)
	if s.Daemon.PID > 0 {
		fmt.Printf(" (pid %d)", s.Daemon.PID)
	}
	fmt.Println()
	fmt.Printf("reachable: %v\n", s.Daemon.Reachable)
	fmt.Println()
	if s.State == "" {
		fmt.Printf("%s   not running\n", s.Project)
	} else {
		fmt.Printf("%s   pod %d   %s\n", s.Project, s.PodID, s.State)
	}
	for _, c := range s.Containers {
		ip := c.IP
		if ip == "" {
			ip = "-"
		}
		fmt.Printf("  %-12s  %-10s  %-10s  %s\n", c.Role, c.Name, c.State, ip)
	}
	if s.Domain != "" {
		fmt.Printf("  https://%s\n", s.Domain)
		for _, port := range s.Ports {
			switch port.Protocol {
			case manifest.PortHTTP:
				fmt.Printf("  https://%s:%d   # %s\n", s.Domain, port.Container, port.Role)
			case manifest.PortTCP:
				if s.PodID == 0 {
					fmt.Printf("  <pending>   # %s\n", port.Role)
				} else {
					host := pod.DerivePort(s.PodID, port.Role)
					fmt.Printf("  127.0.0.1:%d   # %s\n", host, port.Role)
				}
			}
		}
		fmt.Printf("  ssh %s@ssh.tainer.me\n", s.Project)
	}
}

func runStatusProjectJSON(s projectStatusSnapshot) {
	out := struct {
		Daemon     daemonStatus         `json:"daemon"`
		Project    string               `json:"project"`
		PodID      int                  `json:"podId,omitempty"`
		State      string               `json:"state,omitempty"`
		Domain     string               `json:"domain,omitempty"`
		Ports      []manifest.PortEntry `json:"ports,omitempty"`
		Containers []containerStatus    `json:"containers"`
	}{
		Daemon: s.Daemon, Project: s.Project, PodID: s.PodID,
		State: s.State, Domain: s.Domain, Ports: s.Ports,
		Containers: s.Containers,
	}
	if out.Containers == nil {
		out.Containers = []containerStatus{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// statusSnapshot is the resolved view collectStatus returns. Splitting
// data-gathering from rendering keeps each output mode trivial.
type statusSnapshot struct {
	Daemon daemonStatus
	Pods   []podStatus
}

type daemonStatus struct {
	Socket    string `json:"socket"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid,omitempty"`
	Reachable bool   `json:"reachable"`
}

type podStatus struct {
	Name   string
	PodID  int
	State  string
	Domain string               // empty when manifest can't be located
	Ports  []manifest.PortEntry // empty when manifest can't be located
}

func collectStatus(ctx context.Context) statusSnapshot {
	st := runtime.CurrentStatus(ctx, runtime.Options{})
	snap := statusSnapshot{
		Daemon: daemonStatus{
			Socket:    st.Socket,
			Running:   st.Running,
			PID:       st.PID,
			Reachable: st.Reachable,
		},
	}
	if !st.Reachable {
		return snap
	}
	eng, err := engine.New()
	if err != nil {
		return snap
	}
	defer eng.Close()
	pods, err := pod.List(ctx, eng)
	if err != nil {
		return snap
	}
	for _, p := range pods {
		ps := podStatus{Name: p.Name, PodID: p.PodID, State: string(p.State())}
		// Try to fetch the pod's manifest via container labels so we can
		// show the public URL + service ports. Best-effort — a pod
		// without a discoverable manifest still appears, just minimally.
		for _, c := range p.Containers {
			insp, err := eng.Inspect(ctx, c.Name)
			if err == nil && insp.Config != nil {
				if mp := insp.Config.Labels[pod.LabelManifestPath]; mp != "" {
					if m, merr := manifest.Load(mp); merr == nil {
						ps.Domain = m.Project.Domain
						ps.Ports = m.Ports
					}
					break
				}
			}
		}
		snap.Pods = append(snap.Pods, ps)
	}
	return snap
}

func runStatusStyled(s statusSnapshot) {
	tui.Bookend("status")
	muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)

	// --- Daemon block ---
	fmt.Println("  " + muted.Render("Daemon"))
	if s.Daemon.Reachable {
		fmt.Println("    " + tui.MarkSuccess() + "cyberstackd      " + formatDaemonRunningLine(s.Daemon))
		fmt.Println("    " + tui.MarkSuccess() + "socket           " + s.Daemon.Socket)
		fmt.Println("    " + tui.MarkSuccess() + "reachable")
	} else {
		fmt.Println("    " + tui.MarkError() + "cyberstackd      " + formatDaemonRunningLine(s.Daemon))
		fmt.Println("    " + tui.MarkInfo() + "socket           " + s.Daemon.Socket)
		fmt.Println("    " + tui.MarkError() + "not reachable")
		fmt.Println()
		tui.BookendCloseError(0, "Unhealthy", "run `tainer start` from a project dir to bring the stack up")
		return
	}

	// --- Pods block ---
	if len(s.Pods) == 0 {
		fmt.Println()
		fmt.Println("  " + muted.Render("Pods"))
		fmt.Println("    " + tui.MarkInfo() + "no pods running. Run `tainer start` from a project dir to bring one up.")
		fmt.Println()
		tui.BookendClose(0, "Healthy", "0 pods")
		return
	}

	fmt.Println()
	fmt.Println("  " + muted.Render(fmt.Sprintf("Pods (%d)", len(s.Pods))))
	running := 0
	for _, p := range s.Pods {
		if p.State == "running" {
			running++
		}
		renderPodStatusStyled(p, muted)
	}

	fmt.Println()
	tui.BookendClose(0, "Healthy", fmt.Sprintf("%d pods · %d running", len(s.Pods), running))
}

// renderPodStatusStyled prints one pod as a brand-marked block. State
// drives the colour of the mark (running=teal, mixed=orange,
// stopped=muted) so the eye can scan a long list and find the
// not-running ones quickly.
func renderPodStatusStyled(p podStatus, muted lipgloss.Style) {
	mark := tui.MarkInfo()
	switch p.State {
	case "running":
		mark = tui.MarkSuccess()
	case "mixed":
		mark = tui.MarkWarn()
	default:
		mark = tui.MarkInfo()
	}
	bold := lipgloss.NewStyle().Bold(true)
	header := mark + bold.Render(p.Name) + "  " + muted.Render(fmt.Sprintf("pod %d · %s", p.PodID, p.State))
	fmt.Println("    " + header)
	if p.Domain != "" {
		fmt.Println("      " + tui.MarkInfo() + "https://" + p.Domain)
		for _, port := range p.Ports {
			switch port.Protocol {
			case manifest.PortHTTP:
				fmt.Printf("      "+tui.MarkInfo()+"https://%s:%d %s\n", p.Domain, port.Container, muted.Render("("+port.Role+")"))
			case manifest.PortTCP:
				host := pod.DerivePort(p.PodID, port.Role)
				fmt.Printf("      "+tui.MarkInfo()+"127.0.0.1:%d %s\n", host, muted.Render("("+port.Role+")"))
			}
		}
		fmt.Println("      " + tui.MarkInfo() + "ssh " + p.Name + "@ssh.tainer.me")
	}
}

// formatDaemonRunningLine returns the "running (pid X)" or
// "not running" string used in the styled daemon block.
func formatDaemonRunningLine(d daemonStatus) string {
	if d.Running {
		if d.PID > 0 {
			return fmt.Sprintf("running (pid %d)", d.PID)
		}
		return "running"
	}
	return "not running"
}

func runStatusPlain(s statusSnapshot) {
	fmt.Printf("socket:    %s\n", s.Daemon.Socket)
	fmt.Printf("running:   %v", s.Daemon.Running)
	if s.Daemon.PID > 0 {
		fmt.Printf(" (pid %d)", s.Daemon.PID)
	}
	fmt.Println()
	fmt.Printf("reachable: %v\n", s.Daemon.Reachable)
	for _, p := range s.Pods {
		fmt.Println()
		if p.Domain == "" {
			fmt.Printf("%s   pod %d   %s\n", p.Name, p.PodID, p.State)
			continue
		}
		// Reuse the existing canonical formatter to stay byte-compatible
		// with anything scripted against the old plain output.
		fmt.Print(pod.FormatStatus(pod.Pod{
			Name:  p.Name,
			PodID: p.PodID,
		}, p.Domain, p.Ports))
	}
}

func runStatusJSON(s statusSnapshot) {
	type jsonPod struct {
		Name   string               `json:"name"`
		PodID  int                  `json:"podId"`
		State  string               `json:"state"`
		Domain string               `json:"domain,omitempty"`
		Ports  []manifest.PortEntry `json:"ports,omitempty"`
	}
	pods := make([]jsonPod, 0, len(s.Pods))
	for _, p := range s.Pods {
		pods = append(pods, jsonPod{
			Name: p.Name, PodID: p.PodID, State: p.State,
			Domain: p.Domain, Ports: p.Ports,
		})
	}
	out := struct {
		Daemon daemonStatus `json:"daemon"`
		Pods   []jsonPod    `json:"pods"`
	}{Daemon: s.Daemon, Pods: pods}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// cmdInit scaffolds a new tainer project into the current working directory.
//
// Usage:
//
//	tainer init <type>         project name defaults to cwd basename
//	tainer init <type> <name>  explicit project name
//
// Matches legacy tainer 0.2.x: init operates on cwd, no project subdir.
// cmdInit scaffolds a new tainer project into cwd. Forms:
//
// cmdDoctor walks every layer of the stack (daemon → agent → network
// → router → TLS → DNS → pods) and prints one mark per check. --fix
// applies the safe recoveries (start daemon, restart router).
//
// Exit code: 0 when fully healthy (warns allowed), 1 otherwise — so
// scripts and CI can gate on it.
func cmdDoctor(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false)

	opts := doctor.Options{}
	for _, a := range rest {
		switch a {
		case "--fix", "-f":
			opts.Fix = true
		default:
			fmt.Fprintln(os.Stderr, "tainer: unknown flag", a)
			fmt.Fprintln(os.Stderr, "usage: tainer doctor [--fix]")
			os.Exit(2)
		}
	}

	// Generous overall deadline: --fix can cold-boot the VM (~30s) and
	// each pod probe gets its own 5s HTTP timeout on top.
	ctx, cancel := ctxWithTimeout(3 * time.Minute)
	defer cancel()

	// JSON mode: run silently, emit the report as one document.
	if mode == tui.ModeJSON {
		rep := doctor.Run(ctx, opts)
		out := struct {
			doctor.Report
			Healthy bool `json:"healthy"`
		}{rep, rep.Healthy()}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		if !rep.Healthy() {
			os.Exit(1)
		}
		return
	}

	// Styled/plain: render each check as it completes so the user sees
	// progress during the slower probes (VM boot under --fix, HTTP
	// timeouts against dead pods). Plain mode drops marks and color for
	// pipe-friendly `status name detail` columns.
	verb := "doctor"
	if opts.Fix {
		verb = "doctor --fix"
	}
	started := time.Now()
	if mode == tui.ModePlain {
		opts.Progress = func(r doctor.Result) {
			fmt.Printf("%-5s %s %s%s\n", r.Status, doctorPad(r.Name), r.Detail, doctorFixedTag(r))
			if r.Hint != "" && r.Status != doctor.StatusOK {
				fmt.Printf("%-5s %s -> %s\n", "", doctorPad(""), r.Hint)
			}
		}
	} else {
		tui.Bookend(verb)

		// Spinner choreography: Started fires before each check, the
		// matching Progress after. StartLineSpinner has a 200ms grace
		// period, so the sub-second checks never flash a spinner —
		// only the genuinely slow ones (VM cold-boot under --fix, HTTP
		// probes racing their timeout) animate.
		var stopSpin func()
		spinDone := func() {
			if stopSpin != nil {
				stopSpin()
				stopSpin = nil
			}
		}
		opts.Started = func(name string) {
			spinDone()
			stopSpin = tui.StartLineSpinner(name)
		}

		// The pod probes get their own section: the stack checks above
		// describe tainer itself, the pod list describes the user's
		// projects. Header prints lazily before the first pod result.
		muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
		podHeaderDone := false
		opts.Progress = func(r doctor.Result) {
			spinDone()
			if !podHeaderDone && (strings.HasPrefix(r.Name, "pod ") || r.Name == "pods") {
				podHeaderDone = true
				fmt.Println()
				fmt.Println("  " + muted.Render("Running pods"))
			}
			fmt.Println("  " + doctorMark(r.Status) + doctorPad(r.Name) + r.Detail + doctorFixedTag(r))
			if r.Hint != "" && r.Status != doctor.StatusOK {
				fmt.Println("  " + strings.Repeat(" ", 4+doctorNameWidth) + "→ " + r.Hint)
			}
		}
		defer spinDone() // safety net if Run bails between Started and Progress
	}
	rep := doctor.Run(ctx, opts)

	// Stopped pods aren't listed line-by-line (that's `tainer list`);
	// a muted one-liner under the pod section keeps the count visible.
	// Skipped when nothing was probed — the "pods" placeholder result
	// already carries the stopped count in its detail.
	if mode != tui.ModePlain && rep.StoppedPods > 0 && doctorHasPodResults(rep) {
		muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
		fmt.Println("  " + muted.Render(fmt.Sprintf("%d stopped — `tainer list` to see them", rep.StoppedPods)))
	}

	ok, warn, fail, skip := rep.Counts()
	summary := fmt.Sprintf("%d ok", ok)
	if warn > 0 {
		summary += fmt.Sprintf(" · %d warning", warn)
	}
	if fail > 0 {
		summary += fmt.Sprintf(" · %d failed", fail)
	}
	if skip > 0 {
		summary += fmt.Sprintf(" · %d skipped", skip)
	}
	if rep.StoppedPods > 0 {
		summary += fmt.Sprintf(" · %d stopped", rep.StoppedPods)
	}
	if mode == tui.ModePlain {
		if rep.Healthy() {
			fmt.Println("healthy: " + summary)
		} else {
			fmt.Println("unhealthy: " + summary)
			os.Exit(1)
		}
		return
	}
	fmt.Println()
	if rep.Healthy() {
		tui.BookendClose(time.Since(started), "Healthy", summary)
	} else {
		tui.BookendCloseError(time.Since(started), "Unhealthy", summary)
		os.Exit(1)
	}
}

// doctorNameWidth aligns check details in one column; "pod <name>"
// rows may overflow it, which is fine — alignment is a nicety.
const doctorNameWidth = 18

func doctorPad(name string) string {
	if len(name) >= doctorNameWidth {
		return name + " "
	}
	return name + strings.Repeat(" ", doctorNameWidth-len(name))
}

func doctorMark(s doctor.Status) string {
	switch s {
	case doctor.StatusOK:
		return tui.MarkSuccess()
	case doctor.StatusWarn:
		return tui.MarkWarn()
	case doctor.StatusSkip:
		return tui.MarkInfo()
	default:
		return tui.MarkError()
	}
}

func doctorFixedTag(r doctor.Result) string {
	if r.Fixed {
		return " (fixed)"
	}
	return ""
}

// doctorHasPodResults reports whether at least one per-pod probe ran
// (as opposed to the aggregate "pods" placeholder/skip rows).
func doctorHasPodResults(rep doctor.Report) bool {
	for _, r := range rep.Results {
		if strings.HasPrefix(r.Name, "pod ") {
			return true
		}
	}
	return false
}

//	tainer init                  (TUI wizard — TODO: wire up tui/wizard)
//	tainer init <type>           (name defaults to cwd basename)
//	tainer init <type> <name>    (explicit name)
//
// Type accepts canonical names AND common shorthands (wp/nest/next/nuxt/node).
//
// Output flags: --plain (auto when piped), --json, --no-color.
//
// Re-initing on a directory that previously held a tainer project (after
// `tainer destroy --clean`) works: html/, data/, db/ are adopted in
// place and reported as such instead of re-created, so the user can re-
// init without losing their data.
func cmdInit(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false)

	if len(rest) < 1 {
		// Wizard mode — TODO: hand off to pkg/tainer/tui/wizard. For
		// now print a usage hint so users know there are positional
		// forms available.
		fmt.Fprintln(os.Stderr, "tainer init: interactive wizard not yet wired up in 0.9.x — pass <type> [name] positionally:")
		fmt.Fprintln(os.Stderr, "  tainer init wordpress [myproject]")
		fmt.Fprintln(os.Stderr, "  shortcuts: wp, php, node, next, nuxt, nest, react, kompozi")
		os.Exit(2)
	}

	rawType := rest[0]
	canonical, ok := canonicalProjectType(rawType)
	if !ok {
		must(fmt.Errorf("unknown project type %q (one of: wordpress, php, nodejs, nextjs, nuxtjs, nestjs, react, kompozi — shortcuts: wp, node, next, nuxt, nest)", rawType))
	}

	opts := initcmd.Options{Type: canonical}
	if len(rest) >= 2 {
		opts.Name = rest[1]
	}
	switch mode {
	case tui.ModeJSON:
		runInitJSON(opts)
	case tui.ModePlain:
		runInitPlain(opts)
	default:
		runInitStyled(opts)
	}
}

// canonicalProjectType normalises shorthand aliases into the canonical
// manifest.ProjectType values. The manifest validator only accepts the
// canonical names, so we expand here before they hit it.
//
// Single source of truth — when adding a new shortcut, list it here so
// the CLI surface stays consistent with `tainer init --help` output.
func canonicalProjectType(s string) (manifest.ProjectType, bool) {
	switch strings.ToLower(s) {
	case "wordpress", "wp":
		return manifest.TypeWordPress, true
	case "php":
		return manifest.TypePHP, true
	case "nodejs", "node":
		return manifest.TypeNodeJS, true
	case "nextjs", "next":
		return manifest.TypeNextJS, true
	case "nuxtjs", "nuxt":
		return manifest.TypeNuxtJS, true
	case "nestjs", "nest":
		return manifest.TypeNestJS, true
	case "react":
		return manifest.TypeReact, true
	case "kompozi":
		return manifest.TypeKompozi, true
	}
	return "", false
}

func runInitStyled(opts initcmd.Options) {
	started := time.Now()
	res, err := initcmd.Run(opts)
	if err != nil {
		// Open the bookend even on failure so the error block reads as
		// a complete frame instead of a bare error line.
		tui.Bookend(stringOr(opts.Name, "init"), "init")
		fmt.Println()
		tui.BookendCloseError(time.Since(started), "Failed", err.Error())
		os.Exit(1)
	}

	tui.Bookend(res.ProjectName, "init")
	muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
	for _, s := range res.Steps {
		fmt.Println("  " + initStepLine(s, muted))
	}
	tui.BookendClose(time.Since(started), "Ready", "run `tainer start` to bring up the pod")
}

// initStepLine renders one Step as a brand-marked output line.
//
//	created → [✓] in tainer green
//	adopted → [i] in tainer blue with "(adopted)" suffix
//	skipped → [i] in tainer blue with "(skipped)" suffix
func initStepLine(s initcmd.Step, muted lipgloss.Style) string {
	mark := tui.MarkSuccess()
	suffix := ""
	switch s.Action {
	case initcmd.StepAdopted:
		mark = tui.MarkInfo()
		suffix = " " + muted.Render("(adopted)")
	case initcmd.StepSkipped:
		mark = tui.MarkInfo()
		suffix = " " + muted.Render("(skipped)")
	}
	switch s.Kind {
	case initcmd.KindRegistry:
		return mark + "registered as " + s.Path + suffix
	case initcmd.KindGitignore:
		if s.Action == initcmd.StepAdopted {
			return mark + ".gitignore " + muted.Render("(already had marker)")
		}
		return mark + ".gitignore updated"
	default:
		return mark + s.Path + suffix
	}
}

func runInitPlain(opts initcmd.Options) {
	res, err := initcmd.Run(opts)
	if err != nil {
		must(err)
	}
	for _, s := range res.Steps {
		switch s.Kind {
		case initcmd.KindRegistry:
			fmt.Printf("registered as %s\n", s.Path)
		case initcmd.KindGitignore:
			if s.Action == initcmd.StepAdopted {
				fmt.Println(".gitignore (already had marker)")
			} else {
				fmt.Println(".gitignore updated")
			}
		default:
			if s.Action == initcmd.StepAdopted {
				fmt.Printf("%s (adopted)\n", s.Path)
			} else {
				fmt.Println(s.Path)
			}
		}
	}
	fmt.Println("Next: tainer start")
}

func runInitJSON(opts initcmd.Options) {
	res, err := initcmd.Run(opts)
	if err != nil {
		must(err)
	}
	out := struct {
		ProjectName  string         `json:"projectName"`
		ManifestPath string         `json:"manifestPath"`
		Steps        []initcmd.Step `json:"steps"`
	}{
		ProjectName:  res.ProjectName,
		ManifestPath: res.ManifestPath,
		Steps:        res.Steps,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// stringOr returns s if non-empty, otherwise fallback. Used to give the
// failed-init bookend something to label itself with when opts.Name is
// unset (we don't know the final project name until init resolves it).
func stringOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// cmdStart locates the manifest (cwd or ~/projects/<name>), starts
// cyberstackd if needed, and calls pod.Start.
func cmdStart(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false) // no TUI variant for start

	// First-time pulls of multi-hundred-MB images plus VM cold-boot can
	// run past 2 min easily — give it real headroom.
	ctx, cancel := ctxWithTimeout(10 * time.Minute)
	defer cancel()

	manifestPath, projectDir, err := locateManifest(rest)
	must(err)
	m, err := manifest.Load(manifestPath)
	must(err)
	projectName := m.Project.Name

	switch mode {
	case tui.ModeJSON:
		runStartJSON(ctx, manifestPath, projectDir)
	case tui.ModePlain:
		runStartPlain(ctx, manifestPath, projectDir, projectName)
	default:
		runStartStyled(ctx, manifestPath, projectDir, projectName)
	}
}

// runStartStyled is the default human-facing path: bookends, brand
// spinner while pod.Start runs, result laid out with [i] marks, closing
// bookend with the elapsed time. Falls back to a styled error block on
// failure.
func runStartStyled(ctx context.Context, manifestPath, projectDir, projectName string) {
	started := time.Now()
	tui.Bookend(projectName, "start")

	var res *pod.StartResult
	err := tui.RunSpinner("starting pod", runStartWork(ctx, manifestPath, projectDir, &res))
	if err != nil {
		fmt.Println()
		tui.BookendCloseError(time.Since(started), "Failed", err.Error())
		os.Exit(1)
	}

	fmt.Println()
	muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
	for _, h := range res.HTTPServices {
		role := muted.Render("(" + h.Role + ")")
		fmt.Println("  " + tui.MarkInfo() + h.URL + " " + role)
	}
	for _, t := range res.TCPServices {
		role := muted.Render("(" + t.Role + ")")
		fmt.Println("  " + tui.MarkInfo() + t.Host + " " + role)
	}
	fmt.Println("  " + tui.MarkInfo() + "ssh " + res.Pod + "@ssh.tainer.me")

	tui.BookendClose(time.Since(started), "Ready", "https://"+res.Domain)
}

// runStartPlain is the piped/--plain path: same content as the styled
// variant but no ANSI, no spinner, no in-place updates. Designed to be
// readable in `less`, parseable by line-based tools.
func runStartPlain(ctx context.Context, manifestPath, projectDir, projectName string) {
	started := time.Now()
	fmt.Printf("starting %s...\n", projectName)

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	res, err := pod.Start(ctx, eng, pod.StartOptions{
		ManifestPath: manifestPath,
		ProjectDir:   projectDir,
	})
	must(err)

	fmt.Printf("%s started   (pod %d, %.1fs)\n", res.Pod, res.PodID, time.Since(started).Seconds())
	fmt.Printf("  https://%s\n", res.Domain)
	for _, h := range res.HTTPServices {
		fmt.Printf("  %s   # %s\n", h.URL, h.Role)
	}
	for _, t := range res.TCPServices {
		fmt.Printf("  %s   # %s\n", t.Host, t.Role)
	}
	fmt.Printf("  ssh %s@ssh.tainer.me\n", res.Pod)
}

// runStartJSON emits the StartResult as JSON. Useful for scripts; the
// shape mirrors pod.StartResult exactly so future Go consumers can
// unmarshal it directly. Nil service slices are normalised to empty
// arrays so the JSON contract is `[]` (always iterable) rather than
// `null` (consumers have to special-case).
func runStartJSON(ctx context.Context, manifestPath, projectDir string) {
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	res, err := pod.Start(ctx, eng, pod.StartOptions{
		ManifestPath: manifestPath,
		ProjectDir:   projectDir,
	})
	must(err)
	if res.HTTPServices == nil {
		res.HTTPServices = []pod.HTTPSvcResult{}
	}
	if res.TCPServices == nil {
		res.TCPServices = []pod.TCPSvcResult{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
}

// runStartWork captures pod.Start's result into the closure variable
// `res` and returns the error. Wrapped this way so it can be passed to
// tui.RunSpinner which only takes a func() error.
func runStartWork(ctx context.Context, manifestPath, projectDir string, res **pod.StartResult) func() error {
	return func() error {
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			return err
		}
		defer eng.Close()
		r, e := pod.Start(ctx, eng, pod.StartOptions{
			ManifestPath: manifestPath,
			ProjectDir:   projectDir,
		})
		if e != nil {
			return e
		}
		*res = r
		return nil
	}
}

// cmdStop stops a running pod. The pod name is derived from the
// manifest. Output adapts to TTY + flags:
//
//	tainer stop          styled bookends + brand spinner (auto when TTY)
//	tainer stop --plain  plain text, no spinner (default when piped)
//	tainer stop --json   {"pod":"opp","stopped":true,"elapsedMs":2310}
func cmdStop(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false)

	ctx, cancel := ctxWithTimeout(2 * time.Minute)
	defer cancel()

	manifestPath, _, err := locateManifest(rest)
	must(err)
	m, err := manifest.Load(manifestPath)
	must(err)
	projectName := m.Project.Name

	switch mode {
	case tui.ModeJSON:
		runStopJSON(ctx, projectName)
	case tui.ModePlain:
		runStopPlain(ctx, projectName)
	default:
		runStopStyled(ctx, projectName)
	}
}

func runStopStyled(ctx context.Context, projectName string) {
	started := time.Now()
	tui.Bookend(projectName, "stop")

	err := tui.RunSpinner("stopping pod", func() error {
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			return err
		}
		defer eng.Close()
		return pod.Stop(ctx, eng, projectName)
	})
	if err != nil {
		fmt.Println()
		tui.BookendCloseError(time.Since(started), "Failed", err.Error())
		os.Exit(1)
	}
	tui.BookendClose(time.Since(started), "Stopped")
}

func runStopPlain(ctx context.Context, projectName string) {
	started := time.Now()
	fmt.Printf("stopping %s...\n", projectName)
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	must(pod.Stop(ctx, eng, projectName))
	fmt.Printf("stopped %s   (%.1fs)\n", projectName, time.Since(started).Seconds())
}

func runStopJSON(ctx context.Context, projectName string) {
	started := time.Now()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	must(pod.Stop(ctx, eng, projectName))
	out := struct {
		Pod       string `json:"pod"`
		Stopped   bool   `json:"stopped"`
		ElapsedMs int64  `json:"elapsedMs"`
	}{
		Pod:       projectName,
		Stopped:   true,
		ElapsedMs: time.Since(started).Milliseconds(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// cmdDestroy tears down a pod. Flags: --clean, --nuke.
// cmdDestroy tears down a pod with escalating blast radius:
//
//	tainer destroy [project]                stop+remove containers (default; reversible via `tainer start`)
//	tainer destroy [project] --clean        + remove tainer-managed files from project dir (data/ KEPT)
//	tainer destroy [project] --nuke         + remove data/ (irreversible; confirmation required)
//
// Flags:
//
//	--yes / -y       skip confirmations (only --nuke prompts otherwise)
//	--keep-images    don't prune project images that no other pod uses
//	                 (default behavior is to prune; flag is currently a
//	                 no-op — image pruning is queued for a follow-up)
//	--plain          force plain-text output (default when piped)
//	--json           emit a machine-readable result
//	--no-color       drop colors but keep the structured layout
//
// Positional [project] resolves per the locateManifest contract: if
// omitted, walk up from cwd; if given, registry lookup (no cwd hint).
func cmdDestroy(args []string) {
	// Pull our output flags first so they can sit anywhere on the
	// command line; everything else flows to the destroy-specific
	// flag.FlagSet.
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false)

	fs := flag.NewFlagSet("destroy", flag.ExitOnError)
	doClean := fs.Bool("clean", false, "also remove tainer-managed files in project dir (data/ kept)")
	doNuke := fs.Bool("nuke", false, "also remove data/ (irreversible; confirmation required)")
	skipConfirm := fs.Bool("yes", false, "skip confirmations (also: -y)")
	fs.BoolVar(skipConfirm, "y", false, "skip confirmations (alias for --yes)")
	keepImages := fs.Bool("keep-images", false, "do not prune project images (default: prune unused). NOTE: currently a no-op — image pruning is queued for a follow-up.")
	_ = fs.Parse(rest)

	destroyMode := pod.DestroyDefault
	if *doClean {
		destroyMode = pod.DestroyClean
	}
	if *doNuke {
		destroyMode = pod.DestroyNuke
	}

	manifestPath, projectDir, err := locateManifest(fs.Args())
	must(err)
	m, err := manifest.Load(manifestPath)
	must(err)
	projectName := m.Project.Name

	// Only --nuke prompts (data loss is irreversible). --clean is
	// recoverable from version control + a re-init; default just
	// removes containers and is reversible via `tainer start`.
	if destroyMode == pod.DestroyNuke && !*skipConfirm {
		if !confirmNukeStyled(projectName, mode) {
			runDestroyCancelled(projectName, mode)
			return
		}
	}

	ctx, cancel := ctxWithTimeout(2 * time.Minute)
	defer cancel()

	switch mode {
	case tui.ModeJSON:
		runDestroyJSON(ctx, projectName, projectDir, destroyMode, *keepImages)
	case tui.ModePlain:
		runDestroyPlain(ctx, projectName, projectDir, destroyMode, *keepImages)
	default:
		runDestroyStyled(ctx, projectName, projectDir, destroyMode, *keepImages)
	}
}

// destroyModeLabel returns a short human label for the bookend.
func destroyModeLabel(m pod.DestroyMode) string {
	switch m {
	case pod.DestroyClean:
		return "destroy --clean"
	case pod.DestroyNuke:
		return "destroy --nuke"
	default:
		return "destroy"
	}
}

// confirmNukeStyled prompts the user to type the project name to
// confirm an irreversible nuke. Renders with the brand prompt mark
// when the output mode permits color, otherwise falls back to plain
// text so non-TTY paths (CI, scripts) still get a coherent prompt.
// Returns true iff the user typed the name verbatim.
//
// The message names the full blast radius (project + every file in
// the project dir, including the database) so the user isn't surprised
// by what disappears after they hit enter.
func confirmNukeStyled(projectName string, outMode tui.OutputMode) bool {
	if outMode == tui.ModeStyled {
		bold := lipgloss.NewStyle().Bold(true)
		fmt.Print(tui.PromptMark() + "Type " + bold.Render(projectName) + " to permanently wipe this project and all its files, including the database: ")
	} else {
		fmt.Printf("Type %q to permanently wipe this project and all its files, including the database: ", projectName)
	}
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line) == projectName
}

// runDestroyCancelled prints a styled "cancelled" notice for the
// case where the user declined the --nuke confirmation. Format is
// kept symmetric with the success-bookend layout so the output reads
// the same shape regardless of outcome.
func runDestroyCancelled(projectName string, outMode tui.OutputMode) {
	switch outMode {
	case tui.ModeJSON:
		out := struct {
			Pod       string `json:"pod"`
			Destroyed bool   `json:"destroyed"`
			Cancelled bool   `json:"cancelled"`
		}{Pod: projectName, Destroyed: false, Cancelled: true}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	case tui.ModePlain:
		fmt.Println("cancelled")
	default:
		tui.Bookend(projectName, "destroy --nuke")
		fmt.Println("  " + tui.MarkInfo() + "cancelled")
		fmt.Println()
		tui.BookendClose(0, "Cancelled")
	}
}

func runDestroyStyled(ctx context.Context, projectName, projectDir string, destroyMode pod.DestroyMode, keepImages bool) {
	started := time.Now()
	tui.Bookend(projectName, destroyModeLabel(destroyMode))

	err := tui.RunSpinner("destroying pod", func() error {
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			return err
		}
		defer eng.Close()
		return pod.Destroy(ctx, eng, projectName, projectDir, destroyMode, alreadyConfirmedPrompt(projectName))
	})
	if err != nil {
		fmt.Println()
		tui.BookendCloseError(time.Since(started), "Failed", err.Error())
		os.Exit(1)
	}

	if !keepImages {
		// TODO: image pruning. When implemented, log freed bytes on this line.
	}

	tui.BookendClose(time.Since(started), "Destroyed")
}

func runDestroyPlain(ctx context.Context, projectName, projectDir string, destroyMode pod.DestroyMode, keepImages bool) {
	started := time.Now()
	fmt.Printf("destroying %s (%s)...\n", projectName, destroyModeLabel(destroyMode))
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	must(pod.Destroy(ctx, eng, projectName, projectDir, destroyMode, alreadyConfirmedPrompt(projectName)))
	fmt.Printf("destroyed %s   (%.1fs)\n", projectName, time.Since(started).Seconds())
	_ = keepImages // queued for follow-up; flag-plumbing only
}

func runDestroyJSON(ctx context.Context, projectName, projectDir string, destroyMode pod.DestroyMode, keepImages bool) {
	started := time.Now()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	must(pod.Destroy(ctx, eng, projectName, projectDir, destroyMode, alreadyConfirmedPrompt(projectName)))
	out := struct {
		Pod        string `json:"pod"`
		Mode       string `json:"mode"` // "default" | "clean" | "nuke"
		Destroyed  bool   `json:"destroyed"`
		KeepImages bool   `json:"keepImages"`
		ElapsedMs  int64  `json:"elapsedMs"`
	}{
		Pod:        projectName,
		Mode:       destroyModeJSONLabel(destroyMode),
		Destroyed:  true,
		KeepImages: keepImages,
		ElapsedMs:  time.Since(started).Milliseconds(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func destroyModeJSONLabel(m pod.DestroyMode) string {
	switch m {
	case pod.DestroyClean:
		return "clean"
	case pod.DestroyNuke:
		return "nuke"
	default:
		return "default"
	}
}

// alreadyConfirmedPrompt returns a prompt func that immediately replies
// with the pod name, satisfying pod.Destroy's internal confirmNuke
// without re-prompting. The CLI does the actual user-facing
// confirmation up front (via confirmNukeStyled) — pod.Destroy's
// confirm is a backstop for callers that don't.
func alreadyConfirmedPrompt(podName string) func() string {
	return func() string { return podName }
}

// cmdExec runs a command inside a pod container.
//
// Usage:
//
//	tainer exec <project> [<role>] -- <cmd...>
//	tainer exec <project> <cmd...>        (role resolved via ResolveExecRole)
//
// cmdExec runs a command inside a pod container. Forms:
//
//	tainer exec -- <cmd...>                    (cwd walk-up, default role)
//	tainer exec <project> -- <cmd...>          (registry lookup, default role)
//	tainer exec [project] <role> -- <cmd...>   (explicit role)
//
// Flags before the "--" sentinel:
//
//	-t / --tty            allocate a container-side TTY (default: auto — on
//	                      when both stdin and stdout are terminals)
//	-T / --no-tty         force no-TTY (line-buffered pipes)
//	-u / --user <user>    run as user inside the container
//	-w / --workdir <dir>  cd into <dir> before running
//	-e KEY=VAL            add an env var (may repeat)
//
// Exit code is propagated: `tainer exec ... -- <cmd>` returns whatever
// the inner command returned, so scripts can pipe / test naturally.
//
// Brand surface adapts to the mode. TTY (interactive) mode prints a
// single compact one-liner and hands the terminal over; the closing
// bookend appears on exit with the exit code + elapsed. Non-TTY mode
// (piped, redirected, quick commands) prints the full open/close
// bookends around the streamed output.
func cmdExec(args []string) {
	parsed, err := parseExecArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tainer:", err)
		fmt.Fprintln(os.Stderr, "usage: tainer exec [project] [role] [flags] -- <cmd...>")
		os.Exit(2)
	}

	// Resolve which project / role we're operating against. Project
	// name (if given) drives registry lookup; otherwise cwd walk-up.
	var locator []string
	if parsed.projectName != "" {
		locator = []string{parsed.projectName}
	}
	manifestPath, _, err := locateManifest(locator)
	must(err)
	m, err := manifest.Load(manifestPath)
	must(err)
	projectName := m.Project.Name
	role := pod.ResolveExecRole(m.Project.Type, parsed.role)

	// Fill in sensible per-project-type defaults for --user and
	// --workdir so `tainer exec -- wp option get siteurl` picks up
	// www-data + /var/www/html automatically without every user
	// having to remember the UID and path. User-supplied flags always
	// win — the defaults only fill blanks. When we can't infer a
	// default (unusual role, unknown type), we leave the field empty
	// and the container's image WORKDIR / USER apply.
	execUser := parsed.user
	if execUser == "" {
		execUser = pod.DefaultExecUser(m.Project.Type, role)
	}
	execWorkdir := parsed.workdir
	if execWorkdir == "" {
		execWorkdir = pod.DefaultExecWorkdir(m.Project.Type, role)
	}

	// TTY auto-detection: on when both stdin and stdout are terminals
	// AND the user hasn't overridden. --tty forces on; --no-tty forces
	// off. Explicit stdin (heredoc, pipe) drops the default to off.
	//
	// KNOWN LIMITATION: interactive TTY isn't wired up end-to-end yet
	// on the cyberstack side. crun exec --tty allocates a pty inside
	// the container, but its master is not connected to a real pty on
	// the agent's Go side — bash sees a pipe as stdin and tcgetattr
	// fails ("Inappropriate ioctl for device"). Until we use
	// github.com/creack/pty on the agent side to allocate a proper pty
	// pair and hand crun the master, we silently fall back to non-TTY.
	// Non-interactive commands (curl, ls, cat via pipe) all work fine.
	tty := parsed.ttyForced
	if !parsed.ttyExplicit {
		tty = tui.IsTTY(os.Stdin.Fd()) && tui.IsTTY(os.Stdout.Fd())
	}
	if tty {
		// Only warn if the user asked for TTY explicitly (-t / --tty).
		// Auto-detection turning on TTY for something like
		// `tainer exec -- wp option get siteurl` shouldn't print a
		// scary-looking notice when the command still runs fine
		// non-interactively. Task #4 tracks the real pty wiring.
		if parsed.ttyExplicit && parsed.ttyForced {
			fmt.Fprintln(os.Stderr, "tainer: interactive TTY exec is not yet supported (falling back to non-TTY). Bash/wp-shell/tinker sessions may look broken; use `tainer exec -- <cmd>` for non-interactive commands.")
		}
		tty = false
	}

	// Brand surface: one-liner header, always. On exit, closing
	// bookend with exit code + elapsed. Interactive TTY mode skips
	// most brand chrome so we don't fight the subprocess for the
	// terminal — just a single opening line and a single closing line.
	//
	// Typed-wrapper invocations (`tainer wp plugin list`) show the
	// tool as the verb — `opp · wp · plugin list` — rather than
	// leaking the internal exec/role plumbing.
	verb := "exec " + role
	display := strings.Join(parsed.cmd, " ")
	if execWrapperTool != "" && len(parsed.cmd) > 0 && parsed.cmd[0] == execWrapperTool {
		verb = execWrapperTool
		display = strings.Join(parsed.cmd[1:], " ")
	}
	started := time.Now()
	if tty {
		fmt.Println(tui.MarkBrand() + fmt.Sprintf("%s · %s · %s", projectName, verb, display))
	} else {
		tui.Bookend(projectName, verb, display)
	}

	// Long default deadline — user might sit in `bash` for a while.
	// Explicit ctxWithTimeout kept short for the initial engine dial
	// only; from there on we rely on the subprocess to control lifetime.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()

	// If we're going to allocate a TTY on the container, put the local
	// terminal into raw mode too so keystrokes flow through unbuffered
	// and control chars (Ctrl-C, arrow keys) reach the container.
	//
	// CRITICAL: os.Exit() bypasses deferred functions. If any subsequent
	// code panics or we os.Exit before restoring, the user's terminal
	// stays in raw mode and they can't recover without `reset` / `stty
	// sane`. Explicit restore() calls before every exit path, plus a
	// signal handler for Ctrl-C / SIGTERM. defer is a backstop only.
	restore := func() {}
	if tty {
		r, err := makeStdinRaw()
		if err != nil {
			// Fall back to non-TTY rather than failing outright.
			tty = false
		} else {
			restore = r
			installTerminalRestoreOnSignal(restore)
			defer restore()
		}
	}

	// Stdin routing follows Docker's -i semantics:
	//
	//   - When stdin is a pipe/file (echo "..." | tainer exec, or
	//     tainer exec < file), always attach. Users piping input
	//     obviously want the container to see it.
	//   - When stdin is an idle terminal (user just typed `tainer
	//     exec -- ls`), DON'T attach. Otherwise ExecStream blocks on
	//     `<-stdinDone` after the command exits — the stdin-copy
	//     goroutine is stuck reading from the terminal and only
	//     unblocks when the user presses ENTER (or Ctrl-D). That
	//     shows up as "why do I have to press ENTER to get my
	//     prompt back".
	//   - TTY mode (once wired up per task #4) will always attach
	//     stdin — that's the whole point of interactive.
	//
	// -i / --interactive forces attach regardless.
	var stdin io.Reader
	if tty || parsed.interactive || !tui.IsTTY(os.Stdin.Fd()) {
		stdin = os.Stdin
	}

	code, err := pod.Exec(ctx, eng, projectName, role, pod.ExecOptions{
		Cmd:     parsed.cmd,
		User:    execUser,
		WorkDir: execWorkdir,
		Env:     parsed.env,
		Tty:     tty,
		Stdin:   stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
	elapsed := time.Since(started)

	// Restore terminal BEFORE we print anything else. Doing it here as
	// well as via defer guarantees the restore runs before the os.Exit
	// below (which would otherwise skip the defer).
	restore()

	// Closing bookend: exit-code-dependent phrasing. Zero → success
	// (green [✓] Ready). Non-zero → red BookendCloseError so the eye
	// catches failures scrolling through history.
	//
	// In TTY mode a single one-liner keeps things tidy; the container's
	// output likely already scrolled the terminal so we don't want to
	// take up more real estate than necessary.
	label := fmt.Sprintf("exit %d", code)
	if tty {
		if code == 0 {
			fmt.Println(tui.MarkBrand() + label + " · " + fmtElapsedShort(elapsed))
		} else {
			fmt.Println(tui.MarkBrand() + "[failed] " + label + " · " + fmtElapsedShort(elapsed))
		}
	} else {
		if code == 0 {
			tui.BookendClose(elapsed, label)
		} else {
			tui.BookendCloseError(elapsed, "Failed", label)
		}
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "tainer:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// installTerminalRestoreOnSignal wires a goroutine that restores the
// terminal on SIGINT/SIGTERM/SIGHUP. Belt-and-braces with the deferred
// restore(): if the subprocess catches a signal and we exit through an
// unusual path, we still put the terminal back into cooked mode.
//
// The goroutine leaks intentionally on normal exit — the process is
// terminating anyway and the OS will reap it.
func installTerminalRestoreOnSignal(restore func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-ch
		restore()
		// Re-raise the signal so shell scripts see the standard exit
		// status (128+signum). Reset the handler first so the signal
		// terminates us this time.
		signal.Reset(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		os.Exit(130) // 128 + SIGINT (2) — matches shell convention
	}()
}

// wrapperTypes limits each typed wrapper to the project types whose
// app image actually ships the tool. nil means "any type" — the
// container will complain on its own if the binary is missing, but
// for the common tools we can catch the mistake before dialing the
// engine and phrase it in tainer vocabulary.
var wrapperTypes = map[string][]manifest.ProjectType{
	"wp":       {manifest.TypeWordPress},
	"artisan":  {manifest.TypePHP},
	"composer": {manifest.TypePHP, manifest.TypeWordPress},
	"php":      {manifest.TypePHP, manifest.TypeWordPress},
	"npm":      {manifest.TypeNodeJS, manifest.TypeNextJS, manifest.TypeNuxtJS, manifest.TypeNestJS, manifest.TypeReact, manifest.TypeKompozi},
	"yarn":     {manifest.TypeNodeJS, manifest.TypeNextJS, manifest.TypeNuxtJS, manifest.TypeNestJS, manifest.TypeReact, manifest.TypeKompozi},
	"pnpm":     {manifest.TypeNodeJS, manifest.TypeNextJS, manifest.TypeNuxtJS, manifest.TypeNestJS, manifest.TypeReact, manifest.TypeKompozi},
	"node":     {manifest.TypeNodeJS, manifest.TypeNextJS, manifest.TypeNuxtJS, manifest.TypeNestJS, manifest.TypeReact, manifest.TypeKompozi},
}

// cmdExecWrapper implements the typed shortcuts: `tainer wp plugin
// list` == `tainer exec -- wp plugin list`. The project comes from
// the cwd walk-up only (no project positional — it would be ambiguous
// with the tool's own subcommands), and NO tainer flags are parsed:
// everything after the tool name belongs to the tool verbatim. That
// matters because tools like wp-cli define their own --user flag that
// must not be swallowed by tainer's exec parser. Anyone needing
// tainer-side flags (--user, --env, a different role...) drops down
// to the full `tainer exec` form.
func cmdExecWrapper(tool string, args []string) {
	// Friendly type guard before we dial the engine: `tainer wp` in a
	// nextjs project would otherwise surface as a bare "wp: not found"
	// from the container.
	manifestPath, _, err := locateManifest(nil)
	must(err)
	m, err := manifest.Load(manifestPath)
	must(err)
	if allowed, ok := wrapperTypes[tool]; ok {
		match := false
		for _, t := range allowed {
			if m.Project.Type == t {
				match = true
				break
			}
		}
		if !match {
			fmt.Fprintf(os.Stderr, "tainer: `tainer %s` isn't available for %s projects (%s is %s)\n", tool, m.Project.Type, m.Project.Name, m.Project.Type)
			os.Exit(2)
		}
	}
	execWrapperTool = tool
	cmdExec(append([]string{"--", tool}, args...))
}

// execWrapperTool is set by cmdExecWrapper before delegating to
// cmdExec so the brand header reads `opp · wp · plugin list` instead
// of the internal `opp · exec app · wp plugin list`. Empty for direct
// `tainer exec` invocations.
var execWrapperTool string

// execParsedArgs holds the parsed pieces of a `tainer exec ...` invocation.
type execParsedArgs struct {
	projectName string
	role        string
	user        string
	workdir     string
	env         []string
	ttyForced   bool // true value only meaningful when ttyExplicit
	ttyExplicit bool // user passed --tty or --no-tty
	interactive bool // -i / --interactive: force stdin attach even on idle TTY
	cmd         []string
}

// parseExecArgs walks the arg list looking for flags and the "--"
// sentinel that separates them from the command. Everything after
// "--" is the command verbatim (no further flag parsing).
//
// Positional resolution mirrors the docker exec convention we already
// use in status: 0 positionals = cwd-resolved project, 1 = project
// name OR role (ambiguous — treated as project by default), 2 =
// project + role. If a positional matches a registered project name
// we assume it's the project; otherwise we treat it as the role.
func parseExecArgs(args []string) (execParsedArgs, error) {
	var p execParsedArgs
	// Split on the "--" sentinel.
	sentinel := -1
	for i, a := range args {
		if a == "--" {
			sentinel = i
			break
		}
	}
	pre := args
	if sentinel >= 0 {
		pre = args[:sentinel]
		p.cmd = args[sentinel+1:]
	}

	var positional []string
	for i := 0; i < len(pre); i++ {
		a := pre[i]
		switch {
		case a == "-t" || a == "--tty":
			p.ttyForced = true
			p.ttyExplicit = true
		case a == "-T" || a == "--no-tty":
			p.ttyForced = false
			p.ttyExplicit = true
		case a == "-i" || a == "--interactive":
			p.interactive = true
		default:
			// Handle both `--flag value` (space) and `--flag=value`
			// (equals) forms for the value-taking flags. The equals
			// form doesn't conflict with anything and matches the
			// convention we already document/use for tainer's other
			// commands (e.g. --output=file in db export).
			if v, ok := flagValue(a, pre, &i, "-u", "--user"); ok {
				p.user = v
			} else if v, ok := flagValue(a, pre, &i, "-w", "--workdir"); ok {
				p.workdir = v
			} else if v, ok := flagValue(a, pre, &i, "-e", "--env"); ok {
				p.env = append(p.env, v)
			} else {
				positional = append(positional, a)
			}
		}
	}

	// If no "--" was seen but positional args include what looks like
	// a command (contains a non-name-shaped arg), take the tail as cmd.
	// This lets users type `tainer exec opp ls -la` without needing --.
	if sentinel < 0 && len(positional) > 0 {
		// Scan for the first positional that couldn't plausibly be a
		// project or role name (i.e., starts with `-`, contains `/`,
		// or is longer than a typical name). Everything from there on
		// is treated as the command.
		splitAt := -1
		for i, a := range positional {
			if a == "" {
				continue
			}
			if a[0] == '-' || containsRune(a, '/') || len(a) > 24 {
				splitAt = i
				break
			}
		}
		if splitAt >= 0 {
			p.cmd = positional[splitAt:]
			positional = positional[:splitAt]
		}
	}

	// Positional resolution:
	//   0 positionals → project = cwd walk-up, role = default
	//   1 positional  → if registered as a project, it's the project;
	//                   otherwise treat as role
	//   2 positionals → project + role
	switch len(positional) {
	case 0:
	case 1:
		if isRegisteredProject(positional[0]) {
			p.projectName = positional[0]
		} else {
			p.role = positional[0]
		}
	default:
		p.projectName = positional[0]
		p.role = positional[1]
	}

	if len(p.cmd) == 0 {
		return p, fmt.Errorf("no command given (pass one after `--`, e.g. `tainer exec -- bash`)")
	}
	return p, nil
}

// isRegisteredProject reports whether name is present in the tainer
// project registry. Used by parseExecArgs's single-positional
// disambiguation.
func isRegisteredProject(name string) bool {
	all := registry.All()
	_, ok := all[name]
	return ok
}

// flagValue matches arg against any of the given flag names and
// returns the associated value. Handles both the space-separated
// (`--flag value` — value is pre[*i+1] and *i is advanced) and
// equals-joined (`--flag=value` — value comes from arg itself) forms.
// Returns (value, true) on match, ("", false) otherwise.
func flagValue(arg string, pre []string, i *int, names ...string) (string, bool) {
	for _, name := range names {
		if arg == name {
			if *i+1 >= len(pre) {
				return "", true // caller sees empty value; upstream still accepts
			}
			v := pre[*i+1]
			*i++
			return v, true
		}
		if strings.HasPrefix(arg, name+"=") {
			return arg[len(name)+1:], true
		}
	}
	return "", false
}

// containsRune is a tiny helper — strings.ContainsRune is fine but
// only imported below because we already have strings elsewhere.
func containsRune(s string, r rune) bool { return strings.ContainsRune(s, r) }

// fmtElapsedShort trims decimals for display. Sub-100ms is noise.
func fmtElapsedShort(d time.Duration) string {
	if d < 100*time.Millisecond {
		return "instant"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// makeStdinRaw puts the terminal into raw mode so keystrokes flow
// through to the container unbuffered (interactive TTY workflow).
// Returns a restore func the caller must defer.
func makeStdinRaw() (func(), error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}, err
	}
	return func() { _ = term.Restore(fd, oldState) }, nil
}

// cmdList lists all pods known to tainer. Adapts output to the
// terminal context + flags:
//
//	tainer list            auto-pick: TUI if interactive, styled text otherwise
//	tainer list --ui       force TUI even from a less-capable terminal
//	tainer list --plain    force plain ASCII (default when piped)
//	tainer list --json     machine-readable
//	tainer list --no-color drop colors but keep the structured layout
//
// Registry-known projects are the source of truth — every project that
// has been `tainer init`'d shows up here whether or not its pod is
// currently running. The engine is queried lazily (only when the chosen
// output mode actually needs live state) so `tainer list --json` on a
// machine with no running engine still works.
func cmdList(args []string) {
	flags, _ := tui.ParseOutputFlags(args)
	mode := flags.Resolve(true) // list has a TUI implementation

	// Registry walk — always cheap, never touches the engine.
	all := registry.All()

	switch mode {
	case tui.ModeJSON:
		listAsJSON(all)
	case tui.ModePlain:
		listAsPlain(all)
	case tui.ModeStyled:
		listAsStyled(all)
	case tui.ModeTUI:
		listAsTUI(all)
	}
}

// listAsJSON emits the registry + live pod state as a JSON array.
// Stable field names — this is the API for scripts.
func listAsJSON(all map[string]registry.Project) {
	ctx, cancel := ctxWithTimeout(2 * time.Minute)
	defer cancel()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	pods := map[string]pod.State{}
	if err == nil {
		defer eng.Close()
		if ps, lerr := pod.List(ctx, eng); lerr == nil {
			for _, p := range ps {
				pods[p.Name] = p.State()
			}
		}
	}

	type row struct {
		Name   string `json:"name"`
		Type   string `json:"type"`
		Domain string `json:"domain"`
		Path   string `json:"path"`
		Status string `json:"status"`
	}
	out := make([]row, 0, len(all))
	for name, p := range all {
		st := string(pod.StateStopped)
		if s, ok := pods[name]; ok {
			st = string(s)
		}
		out = append(out, row{Name: name, Type: p.Type, Domain: p.Domain, Path: p.Path, Status: st})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// listAsPlain emits a space-aligned table with no colors or box drawing.
// Readable when sent to `less`, still trivially parseable from scripts
// (whitespace-separated, no embedded spaces in fields). For machines
// that need stricter parsing, --json is the right form.
func listAsPlain(all map[string]registry.Project) {
	ctx, cancel := ctxWithTimeout(2 * time.Minute)
	defer cancel()
	pods := podStateMap(ctx, all)

	names := sortedKeys(all)
	if len(names) == 0 {
		return
	}

	// Discover widest cell per column so the table aligns. The header
	// floors each column at its own label width.
	nameW, typeW, domainW := len("NAME"), len("TYPE"), len("DOMAIN")
	for _, n := range names {
		if l := len(n); l > nameW {
			nameW = l
		}
		if l := len(all[n].Type); l > typeW {
			typeW = l
		}
		if l := len(all[n].Domain); l > domainW {
			domainW = l
		}
	}
	fmt.Printf("%-*s  %-*s  %-*s  %s\n", nameW, "NAME", typeW, "TYPE", domainW, "DOMAIN", "STATUS")
	for _, name := range names {
		p := all[name]
		st := pods[name]
		if st == "" {
			st = string(pod.StateStopped)
		}
		fmt.Printf("%-*s  %-*s  %-*s  %s\n", nameW, name, typeW, p.Type, domainW, p.Domain, st)
	}
}

// listAsStyled emits a one-shot styled report — bookends + a colored
// table — without taking over the terminal. The right shape for "I just
// want to see what's there".
func listAsStyled(all map[string]registry.Project) {
	if len(all) == 0 {
		tui.Bookend("pods")
		fmt.Println("  " + tui.MarkInfo() + "No projects yet. Run `tainer init` to create one.")
		fmt.Println()
		return
	}

	ctx, cancel := ctxWithTimeout(2 * time.Minute)
	defer cancel()
	pods := podStateMap(ctx, all)

	tui.Bookend("pods")
	names := sortedKeys(all)

	// Column widths: pick the longest in each column, capped.
	nameW, typeW, domainW := 4, 4, 6
	for _, n := range names {
		if l := len(n); l > nameW {
			nameW = l
		}
		if l := len(all[n].Type); l > typeW {
			typeW = l
		}
		if l := len(all[n].Domain); l > domainW {
			domainW = l
		}
	}
	header := fmt.Sprintf("  %-*s   %-*s   %-*s   %s", nameW, "NAME", typeW, "TYPE", domainW, "DOMAIN", "STATUS")
	fmt.Println(listHeaderStyle().Render(header))

	running := 0
	for _, name := range names {
		p := all[name]
		st := pods[name]
		if st == "" {
			st = string(pod.StateStopped)
		}
		if st == string(pod.StateRunning) {
			running++
		}
		status := renderStatus(st)
		fmt.Printf("  %-*s   %-*s   %-*s   %s\n",
			nameW, name,
			typeW, p.Type,
			domainW, p.Domain,
			status)
	}

	tui.BookendClose(0, fmt.Sprintf("%d pods · %d running", len(all), running))
}

// listAsTUI hands off to the existing Bubble Tea list view.
func listAsTUI(all map[string]registry.Project) {
	if len(all) == 0 {
		// Empty state — the TUI has nothing to show, so bail to styled.
		listAsStyled(all)
		return
	}

	projects := make([]tuilist.Project, 0, len(all))
	for name, p := range all {
		projects = append(projects, tuilist.Project{
			Name:   name,
			Type:   p.Type,
			Domain: p.Domain,
			Path:   p.Path,
		})
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })

	// The TUI calls fetch in a goroutine and waits for the result via
	// a dataLoadedMsg. If the goroutine panics or blocks forever, the
	// TUI sits in its loading state and looks frozen (no keys map). To
	// make sure the TUI ALWAYS leaves the loading state, we wrap the
	// fetch with: (a) a hard 5s deadline backed by a sentinel result on
	// timeout, (b) a panic recovery that returns an empty result, and
	// (c) running the engine probe in its own goroutine so a hung Docker
	// API call can't tar-pit the whole render.
	fetch := func(names []string) (map[string]string, bool, int) {
		type result struct {
			statuses map[string]string
		}
		ch := make(chan result, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintln(os.Stderr, "tainer: list fetch panic:", r)
					ch <- result{statuses: map[string]string{}}
				}
			}()
			ctx, cancel := ctxWithTimeout(5 * time.Second)
			defer cancel()
			statuses := map[string]string{}
			eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
			if err != nil {
				ch <- result{statuses: statuses}
				return
			}
			defer eng.Close()
			ps, lerr := pod.List(ctx, eng)
			if lerr != nil {
				ch <- result{statuses: statuses}
				return
			}
			for _, p := range ps {
				s := string(p.State())
				if s == "running" {
					statuses[p.Name] = "Running"
				} else {
					statuses[p.Name] = "stopped"
				}
			}
			ch <- result{statuses: statuses}
		}()

		select {
		case r := <-ch:
			return r.statuses, false, 0
		case <-time.After(6 * time.Second):
			// Hard ceiling — TUI MUST leave loading state.
			return map[string]string{}, false, 0
		}
	}

	if _, err := tuilist.Run(projects, fetch); err != nil {
		must(err)
	}
}

// podStateMap is a cheap helper that queries the engine for live pod
// states and returns name→state. Returns an empty map (not an error)
// if the engine isn't reachable — list is allowed to render registry
// data without live state if the daemon is down.
func podStateMap(ctx context.Context, _ map[string]registry.Project) map[string]string {
	out := map[string]string{}
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	if err != nil {
		return out
	}
	defer eng.Close()
	ps, lerr := pod.List(ctx, eng)
	if lerr != nil {
		return out
	}
	for _, p := range ps {
		out[p.Name] = string(p.State())
	}
	return out
}

func sortedKeys(m map[string]registry.Project) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// listHeaderStyle returns the muted table-header style.
func listHeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(tui.Colors().Muted).
		Bold(true)
}

// renderStatus colors the status word per the brand semantics:
//
//	running → teal      (success/healthy)
//	mixed   → orange    (transitional/attention)
//	stopped → muted     (neutral, not a failure)
func renderStatus(st string) string {
	c := tui.Colors()
	switch st {
	case string(pod.StateRunning):
		return lipgloss.NewStyle().Foreground(c.Teal).Render("● running")
	case string(pod.StateMixed):
		return lipgloss.NewStyle().Foreground(c.Orange).Render("● mixed")
	default:
		return lipgloss.NewStyle().Foreground(c.Muted).Render("○ stopped")
	}
}

// cmdDB handles `tainer db export` and `tainer db import`.
// cmdDB dispatches the `tainer db <export|import>` subcommands.
//
// Both subcommands follow the locateManifest convention:
//   - When no project name is given, the cwd walk-up resolves the
//     project (so `tainer db export` Just Works from inside a project).
//   - When a project name is given, the registry lookup resolves it
//     (so you can dump/restore from anywhere).
//
// Output flags (--plain / --json / --no-color) flow into each
// subcommand via tui.ParseOutputFlags.
func cmdDB(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: tainer db export [project] [file]")
		fmt.Fprintln(os.Stderr, "       tainer db import [project] <file>")
		os.Exit(2)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "export":
		cmdDBExport(rest)
	case "import":
		cmdDBImport(rest)
	default:
		fmt.Fprintln(os.Stderr, "tainer: unknown db subcommand:", sub)
		os.Exit(2)
	}
}

// dbExportArgs captures the resolved arguments for an export run.
type dbExportArgs struct {
	manifestPath string
	manifest     *manifest.Manifest
	outPath      string
}

// resolveDBExportArgs implements the positional-arg disambiguation
// for `tainer db export`. Forms:
//
//   - 0 positional + cwd is a project: cwd project, auto-named file
//   - 1 positional: project name, auto-named file
//   - 2 positional: project name + explicit output file
//   - --output / -o overrides the file in any of the above
func resolveDBExportArgs(positional []string, outputFlag string) (*dbExportArgs, error) {
	var locator []string
	if len(positional) >= 1 {
		locator = []string{positional[0]}
	}
	manifestPath, _, err := locateManifest(locator)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	out := outputFlag
	if out == "" && len(positional) >= 2 {
		out = positional[1]
	}
	if out == "" {
		out = fmt.Sprintf("%s-%s.sql",
			m.Project.Name,
			time.Now().UTC().Format("2006-01-02T15-04-05Z"))
	}
	return &dbExportArgs{manifestPath: manifestPath, manifest: m, outPath: out}, nil
}

func cmdDBExport(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false)

	fs := flag.NewFlagSet("db export", flag.ExitOnError)
	outputFlag := fs.String("output", "", "write dump to this path (default: <project>-<timestamp>.sql in cwd)")
	fs.StringVar(outputFlag, "o", "", "alias for --output")
	_ = fs.Parse(rest)

	resolved, err := resolveDBExportArgs(fs.Args(), *outputFlag)
	must(err)

	projectName := resolved.manifest.Project.Name

	switch mode {
	case tui.ModeJSON:
		runDBExportJSON(projectName, resolved)
	case tui.ModePlain:
		runDBExportPlain(projectName, resolved)
	default:
		runDBExportStyled(projectName, resolved)
	}
}

func runDBExportStyled(projectName string, r *dbExportArgs) {
	started := time.Now()
	tui.Bookend(projectName, "db export")

	err := tui.RunSpinner("exporting database", func() error {
		ctx, cancel := ctxWithTimeout(5 * time.Minute)
		defer cancel()
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			return err
		}
		defer eng.Close()
		return pod.DBExport(ctx, eng, projectName, r.manifest, r.outPath)
	})
	if err != nil {
		fmt.Println()
		tui.BookendCloseError(time.Since(started), "Failed", err.Error())
		os.Exit(1)
	}

	muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
	abs := absPath(r.outPath)
	sizeNote := ""
	if size, ok := fileSizeHuman(abs); ok {
		sizeNote = " " + muted.Render("("+size+")")
	}
	fmt.Println()
	fmt.Println("  " + tui.MarkInfo() + abs + sizeNote)
	tui.BookendClose(time.Since(started), "Exported")
}

func runDBExportPlain(projectName string, r *dbExportArgs) {
	started := time.Now()
	fmt.Printf("exporting %s...\n", projectName)
	ctx, cancel := ctxWithTimeout(5 * time.Minute)
	defer cancel()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	must(pod.DBExport(ctx, eng, projectName, r.manifest, r.outPath))
	fmt.Printf("exported %s → %s   (%.1fs)\n", projectName, r.outPath, time.Since(started).Seconds())
}

func runDBExportJSON(projectName string, r *dbExportArgs) {
	started := time.Now()
	ctx, cancel := ctxWithTimeout(5 * time.Minute)
	defer cancel()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	must(pod.DBExport(ctx, eng, projectName, r.manifest, r.outPath))
	abs := absPath(r.outPath)
	size, _ := fileSizeBytes(abs)
	out := struct {
		Pod       string `json:"pod"`
		File      string `json:"file"`
		Bytes     int64  `json:"bytes"`
		ElapsedMs int64  `json:"elapsedMs"`
	}{
		Pod:       projectName,
		File:      abs,
		Bytes:     size,
		ElapsedMs: time.Since(started).Milliseconds(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// dbImportArgs captures the resolved arguments for an import run.
type dbImportArgs struct {
	manifestPath string
	manifest     *manifest.Manifest
	inPath       string
}

// resolveDBImportArgs implements the positional-arg disambiguation
// for `tainer db import`. Forms:
//
//   - 1 positional: cwd project + given file
//   - 2 positional: project name + file
func resolveDBImportArgs(positional []string) (*dbImportArgs, error) {
	switch len(positional) {
	case 0:
		return nil, fmt.Errorf("missing input file (usage: tainer db import [project] <file>)")
	case 1:
		// File only — cwd resolves the project.
		manifestPath, _, err := locateManifest(nil)
		if err != nil {
			return nil, err
		}
		m, err := manifest.Load(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("reading manifest: %w", err)
		}
		return &dbImportArgs{manifestPath: manifestPath, manifest: m, inPath: positional[0]}, nil
	default:
		// project + file.
		manifestPath, _, err := locateManifest(positional[:1])
		if err != nil {
			return nil, err
		}
		m, err := manifest.Load(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("reading manifest: %w", err)
		}
		return &dbImportArgs{manifestPath: manifestPath, manifest: m, inPath: positional[1]}, nil
	}
}

func cmdDBImport(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false)

	resolved, err := resolveDBImportArgs(rest)
	must(err)

	// Resolve the input path early so the styled output can show the
	// absolute target the import will read from.
	abs := absPath(resolved.inPath)
	if _, err := os.Stat(abs); err != nil {
		must(fmt.Errorf("input file not readable: %w", err))
	}
	resolved.inPath = abs

	projectName := resolved.manifest.Project.Name

	switch mode {
	case tui.ModeJSON:
		runDBImportJSON(projectName, resolved)
	case tui.ModePlain:
		runDBImportPlain(projectName, resolved)
	default:
		runDBImportStyled(projectName, resolved)
	}
}

func runDBImportStyled(projectName string, r *dbImportArgs) {
	started := time.Now()
	tui.Bookend(projectName, "db import")

	muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
	sizeNote := ""
	if size, ok := fileSizeHuman(r.inPath); ok {
		sizeNote = " " + muted.Render("("+size+")")
	}
	fmt.Println("  " + tui.MarkInfo() + r.inPath + sizeNote)
	fmt.Println()

	err := tui.RunSpinner("importing database", func() error {
		ctx, cancel := ctxWithTimeout(5 * time.Minute)
		defer cancel()
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			return err
		}
		defer eng.Close()
		return pod.DBImport(ctx, eng, projectName, r.manifest, r.inPath)
	})
	if err != nil {
		fmt.Println()
		tui.BookendCloseError(time.Since(started), "Failed", err.Error())
		os.Exit(1)
	}

	tui.BookendClose(time.Since(started), "Imported")
}

func runDBImportPlain(projectName string, r *dbImportArgs) {
	started := time.Now()
	fmt.Printf("importing %s ← %s...\n", projectName, r.inPath)
	ctx, cancel := ctxWithTimeout(5 * time.Minute)
	defer cancel()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	must(pod.DBImport(ctx, eng, projectName, r.manifest, r.inPath))
	fmt.Printf("imported %s ← %s   (%.1fs)\n", projectName, r.inPath, time.Since(started).Seconds())
}

func runDBImportJSON(projectName string, r *dbImportArgs) {
	started := time.Now()
	ctx, cancel := ctxWithTimeout(5 * time.Minute)
	defer cancel()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()
	must(pod.DBImport(ctx, eng, projectName, r.manifest, r.inPath))
	size, _ := fileSizeBytes(r.inPath)
	out := struct {
		Pod       string `json:"pod"`
		File      string `json:"file"`
		Bytes     int64  `json:"bytes"`
		ElapsedMs int64  `json:"elapsedMs"`
	}{
		Pod:       projectName,
		File:      r.inPath,
		Bytes:     size,
		ElapsedMs: time.Since(started).Milliseconds(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// absPath returns an absolute version of p. Falls back to p unchanged
// if it can't be resolved (shouldn't happen for paths we created
// ourselves; defensive for user-supplied input).
func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// fileSizeBytes returns the file size in bytes. (size, true) on success,
// (0, false) when the file isn't stat-able (e.g. just deleted).
func fileSizeBytes(p string) (int64, bool) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, false
	}
	return st.Size(), true
}

// fileSizeHuman returns a short, glanceable size string for a file:
// "743 B", "12.3 KB", "8.4 MB", "1.2 GB". Uses 1024-based units to
// match what `ls -lh`, `du -h`, etc. show on macOS — the human eye
// scans these faster than raw bytes when the dump is multi-MB.
func fileSizeHuman(p string) (string, bool) {
	n, ok := fileSizeBytes(p)
	if !ok {
		return "", false
	}
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb)), true
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb)), true
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb)), true
	default:
		return fmt.Sprintf("%d B", n), true
	}
}

// cmdUpdate refreshes images for one or all pods. Three modes:
//
//	tainer update                project: cwd walk-up resolves the project
//	tainer update <project>      project: registry lookup
//	tainer update --all          iterate every registered pod, update each
//	tainer update --base <type>  pull just the base image(s) for <type>
//
// Output flags: --plain (auto when piped), --json, --no-color.
//
// For --all, per-pod failures don't abort: each failure renders as a
// [✗] line and the run continues. The process exits non-zero if any
// pod failed so scripts can branch on it.
func cmdUpdate(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false)

	fs := flag.NewFlagSet("update", flag.ExitOnError)
	baseType := fs.String("base", "", "refresh base image for project type (skips pod-level update)")
	all := fs.Bool("all", false, "update every registered pod sequentially")
	_ = fs.Parse(rest)

	switch {
	case *baseType != "":
		runUpdateBase(mode, *baseType)
	case *all:
		runUpdateAll(mode)
	default:
		runUpdateSingle(mode, fs.Args())
	}
}

// runUpdateBase refreshes ONLY the base image for a project type
// (e.g. `tainer update --base wordpress`). No pod-level changes; useful
// after a base image rebuild upstream when you want to pre-cache it
// before the next `tainer start`.
func runUpdateBase(outMode tui.OutputMode, rawType string) {
	canonical, ok := canonicalProjectType(rawType)
	if !ok {
		must(fmt.Errorf("unknown project type %q (one of: wordpress, php, nodejs, nextjs, nuxtjs, nestjs, react, kompozi)", rawType))
	}
	started := time.Now()
	label := "refreshing base image for " + string(canonical)

	run := func() error {
		ctx, cancel := ctxWithTimeout(10 * time.Minute)
		defer cancel()
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			return err
		}
		defer eng.Close()
		return pod.Update(ctx, eng, pod.StartOptions{}, pod.UpdateOptions{
			BaseOnly: true,
			BaseType: canonical,
		})
	}

	switch outMode {
	case tui.ModeJSON:
		err := run()
		emitUpdateBaseJSON(string(canonical), err, time.Since(started))
		if err != nil {
			os.Exit(1)
		}
	case tui.ModePlain:
		fmt.Printf("%s...\n", label)
		must(run())
		fmt.Printf("updated base image for %s   (%.1fs)\n", canonical, time.Since(started).Seconds())
	default:
		tui.Bookend(string(canonical), "update --base")
		if err := tui.RunSpinner(label, run); err != nil {
			fmt.Println()
			tui.BookendCloseError(time.Since(started), "Failed", err.Error())
			os.Exit(1)
		}
		tui.BookendClose(time.Since(started), "Updated")
	}
}

func emitUpdateBaseJSON(baseType string, err error, elapsed time.Duration) {
	out := struct {
		Mode      string `json:"mode"`
		BaseType  string `json:"baseType"`
		Updated   bool   `json:"updated"`
		Error     string `json:"error,omitempty"`
		ElapsedMs int64  `json:"elapsedMs"`
	}{
		Mode:      "base",
		BaseType:  baseType,
		Updated:   err == nil,
		ElapsedMs: elapsed.Milliseconds(),
	}
	if err != nil {
		out.Error = err.Error()
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// runUpdateSingle refreshes the images for one specific pod.
// Positional args follow the locateManifest contract (empty → cwd
// walk-up; single arg → registry lookup).
func runUpdateSingle(outMode tui.OutputMode, positional []string) {
	manifestPath, projectDir, err := locateManifest(positional)
	must(err)
	m, err := manifest.Load(manifestPath)
	must(err)
	projectName := m.Project.Name

	started := time.Now()
	label := "updating images for " + projectName

	run := func() error {
		ctx, cancel := ctxWithTimeout(10 * time.Minute)
		defer cancel()
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			return err
		}
		defer eng.Close()
		return pod.Update(ctx, eng,
			pod.StartOptions{ManifestPath: manifestPath, ProjectDir: projectDir},
			pod.UpdateOptions{})
	}

	switch outMode {
	case tui.ModeJSON:
		err := run()
		emitUpdateSingleJSON(projectName, err, time.Since(started))
		if err != nil {
			os.Exit(1)
		}
	case tui.ModePlain:
		fmt.Printf("%s...\n", label)
		must(run())
		fmt.Printf("updated %s   (%.1fs)\n", projectName, time.Since(started).Seconds())
	default:
		tui.Bookend(projectName, "update")
		if err := tui.RunSpinner(label, run); err != nil {
			fmt.Println()
			tui.BookendCloseError(time.Since(started), "Failed", err.Error())
			os.Exit(1)
		}
		tui.BookendClose(time.Since(started), "Updated")
	}
}

func emitUpdateSingleJSON(projectName string, err error, elapsed time.Duration) {
	out := struct {
		Mode      string `json:"mode"`
		Pod       string `json:"pod"`
		Updated   bool   `json:"updated"`
		Error     string `json:"error,omitempty"`
		ElapsedMs int64  `json:"elapsedMs"`
	}{
		Mode:      "project",
		Pod:       projectName,
		Updated:   err == nil,
		ElapsedMs: elapsed.Milliseconds(),
	}
	if err != nil {
		out.Error = err.Error()
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// updateAllPodResult is one row in the --all summary; same shape used
// by all three output modes.
type updateAllPodResult struct {
	Name      string `json:"name"`
	Updated   bool   `json:"updated"`
	Error     string `json:"error,omitempty"`
	ElapsedMs int64  `json:"elapsedMs"`
}

// runUpdateAll iterates every registered project and updates each.
// Sequential so the engine isn't asked to pull multiple multi-hundred-MB
// images in parallel. Per-pod failures are non-fatal but the process
// exits 1 if any pod failed.
func runUpdateAll(outMode tui.OutputMode) {
	all := registry.All()
	if len(all) == 0 {
		switch outMode {
		case tui.ModeJSON:
			fmt.Println(`{"mode":"all","pods":[],"totalElapsedMs":0}`)
		case tui.ModePlain:
			fmt.Println("no projects registered")
		default:
			tui.Bookend("update --all")
			fmt.Println("  " + tui.MarkInfo() + "no projects registered. Run `tainer init` to create one.")
			fmt.Println()
			tui.BookendClose(0, "Done", "0 updated")
		}
		return
	}

	// Stable order so identical runs produce identical output (helps
	// with `diff`-based comparisons in CI / debugging).
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	overallStarted := time.Now()
	results := make([]updateAllPodResult, 0, len(names))
	anyFailed := false

	// In styled mode the bookend frame opens once and each pod gets its
	// own spinner-into-mark line. In plain mode we just stream lines.
	if outMode == tui.ModeStyled {
		tui.Bookend("update --all")
	}

	for _, name := range names {
		p := all[name]
		manifestPath := filepath.Join(p.Path, manifest.FileName)
		// If the manifest is gone (e.g. dir deleted out-of-band) we
		// can't update — record the failure and move on.
		if _, err := os.Stat(manifestPath); err != nil {
			res := updateAllPodResult{Name: name, Updated: false, Error: "manifest missing: " + err.Error()}
			results = append(results, res)
			anyFailed = true
			renderUpdateAllLine(outMode, res)
			continue
		}

		started := time.Now()
		label := "updating " + name

		runErr := func() error {
			ctx, cancel := ctxWithTimeout(10 * time.Minute)
			defer cancel()
			eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
			if err != nil {
				return err
			}
			defer eng.Close()
			return pod.Update(ctx, eng,
				pod.StartOptions{ManifestPath: manifestPath, ProjectDir: p.Path},
				pod.UpdateOptions{})
		}

		var err error
		switch outMode {
		case tui.ModeStyled:
			err = tui.RunSpinner(label, runErr)
		case tui.ModePlain:
			fmt.Printf("%s...\n", label)
			err = runErr()
		default: // JSON: silent execution, render at end
			err = runErr()
		}

		res := updateAllPodResult{
			Name:      name,
			Updated:   err == nil,
			ElapsedMs: time.Since(started).Milliseconds(),
		}
		if err != nil {
			res.Error = err.Error()
			anyFailed = true
		}
		results = append(results, res)

		if outMode == tui.ModePlain {
			if res.Updated {
				fmt.Printf("updated %s   (%.1fs)\n", name, float64(res.ElapsedMs)/1000)
			} else {
				fmt.Printf("failed %s: %s\n", name, res.Error)
			}
		}
	}

	totalElapsed := time.Since(overallStarted)
	switch outMode {
	case tui.ModeJSON:
		out := struct {
			Mode           string               `json:"mode"`
			Pods           []updateAllPodResult `json:"pods"`
			TotalElapsedMs int64                `json:"totalElapsedMs"`
		}{
			Mode:           "all",
			Pods:           results,
			TotalElapsedMs: totalElapsed.Milliseconds(),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	case tui.ModeStyled:
		updated := 0
		for _, r := range results {
			if r.Updated {
				updated++
			}
		}
		fmt.Println()
		summary := fmt.Sprintf("%d updated", updated)
		if anyFailed {
			summary += fmt.Sprintf(", %d failed", len(results)-updated)
		}
		if anyFailed {
			tui.BookendCloseError(totalElapsed, "Done with errors", summary)
		} else {
			tui.BookendClose(totalElapsed, "Done", summary)
		}
	}

	if anyFailed {
		os.Exit(1)
	}
}

// renderUpdateAllLine prints a single failure line for the cases where
// we couldn't even attempt the update (e.g. missing manifest). Used by
// styled + plain modes; JSON aggregates and prints at the end.
func renderUpdateAllLine(outMode tui.OutputMode, res updateAllPodResult) {
	if res.Updated {
		return
	}
	switch outMode {
	case tui.ModeStyled:
		muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
		fmt.Println("  " + tui.MarkError() + res.Name + " " + muted.Render(res.Error))
	case tui.ModePlain:
		fmt.Printf("failed %s: %s\n", res.Name, res.Error)
	}
}

// cmdNetwork handles `tainer network mode show|set <perf|compat|hybrid>`.
// Show is a pure read; Set runs the full stop-pods → swap-daemon →
// restart-pods dance with a spinner over the slow bit.
//
// Output flags: --plain (auto when piped), --json, --no-color.
func cmdNetwork(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := flags.Resolve(false)

	if len(rest) < 2 || rest[0] != "mode" {
		fmt.Fprintln(os.Stderr, "usage: tainer network mode show|set <perf|compat|hybrid>")
		os.Exit(2)
	}
	switch rest[1] {
	case "show":
		runNetworkShow(mode)
	case "set":
		if len(rest) < 3 {
			fmt.Fprintln(os.Stderr, "usage: tainer network mode set <perf|compat|hybrid>")
			os.Exit(2)
		}
		runNetworkSet(mode, rest[2])
	default:
		fmt.Fprintln(os.Stderr, "tainer: unknown network subcommand:", rest[1])
		os.Exit(2)
	}
}

func runNetworkShow(outMode tui.OutputMode) {
	cur, err := networkcmd.Current()
	must(err)
	switch outMode {
	case tui.ModeJSON:
		out := struct {
			Mode    string `json:"mode"`
			Display string `json:"display"`
		}{Mode: string(cur), Display: cur.Display()}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	case tui.ModePlain:
		fmt.Printf("network mode: %s\n", cur.Display())
	default:
		tui.Bookend("network")
		fmt.Println("  " + tui.MarkInfo() + "mode: " + cur.Display())
		fmt.Println()
		tui.BookendClose(0, "OK")
	}
}

func runNetworkSet(outMode tui.OutputMode, raw string) {
	started := time.Now()
	var res *networkcmd.SwitchResult

	run := func() error {
		ctx, cancel := ctxWithTimeout(60 * time.Second)
		defer cancel()
		r, err := networkcmd.Switch(ctx, raw)
		res = r
		return err
	}

	switch outMode {
	case tui.ModeJSON:
		err := run()
		emitNetworkSetJSON(res, err, time.Since(started))
		if err != nil {
			os.Exit(1)
		}
	case tui.ModePlain:
		// Plain mode: short status line on entry, simple summary after.
		// Caller might not even know what "current" was, so include it.
		if curr, err := networkcmd.Current(); err == nil {
			fmt.Printf("switching network mode (current: %s)...\n", curr.Display())
		} else {
			fmt.Println("switching network mode...")
		}
		must(run())
		if res != nil && res.NoChange {
			fmt.Printf("already in %s mode\n", res.New.Display())
			return
		}
		fmt.Printf("switched %s → %s   (%.1fs)\n", res.Previous, res.New, time.Since(started).Seconds())
		if len(res.Restarted) > 0 {
			fmt.Printf("  restarted: %v\n", res.Restarted)
		}
		if len(res.Failed) > 0 {
			fmt.Printf("  failed:    %v\n", res.Failed)
		}
	default:
		tui.Bookend("network set " + raw)
		err := tui.RunSpinner("switching network mode", run)
		if err != nil {
			fmt.Println()
			tui.BookendCloseError(time.Since(started), "Failed", err.Error())
			os.Exit(1)
		}

		if res != nil && res.NoChange {
			fmt.Println()
			fmt.Println("  " + tui.MarkInfo() + "already in " + res.New.Display())
			fmt.Println()
			tui.BookendClose(time.Since(started), "No change")
			return
		}

		muted := lipgloss.NewStyle().Foreground(tui.Colors().Muted)
		fmt.Println()
		fmt.Println("  " + tui.MarkSuccess() + string(res.Previous) + muted.Render(" → ") + string(res.New))
		if len(res.Restarted) > 0 {
			fmt.Println("  " + tui.MarkSuccess() + fmt.Sprintf("restarted %d pod(s): %s", len(res.Restarted), strings.Join(res.Restarted, ", ")))
		}
		for _, f := range res.Failed {
			fmt.Println("  " + tui.MarkError() + f)
		}
		tui.BookendClose(time.Since(started), "Switched")
	}
}

func emitNetworkSetJSON(res *networkcmd.SwitchResult, err error, elapsed time.Duration) {
	out := struct {
		Switched  bool     `json:"switched"`
		NoChange  bool     `json:"noChange"`
		Previous  string   `json:"previous,omitempty"`
		New       string   `json:"new,omitempty"`
		Restarted []string `json:"restarted,omitempty"`
		Failed    []string `json:"failed,omitempty"`
		Error     string   `json:"error,omitempty"`
		ElapsedMs int64    `json:"elapsedMs"`
	}{ElapsedMs: elapsed.Milliseconds()}
	if res != nil {
		out.Previous = string(res.Previous)
		out.New = string(res.New)
		out.NoChange = res.NoChange
		out.Restarted = res.Restarted
		out.Failed = res.Failed
	}
	if err != nil {
		out.Error = err.Error()
		out.Switched = false
	} else {
		out.Switched = !out.NoChange
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
