// tainer-menu — the menu bar face of the tainer stack.
//
// A tiny systray app that consumes the `tainer watch --json` event
// stream (the daemon owns the recovering/failed timing) and renders it
// as the brand mark in the macOS menu bar:
//
//	full colour       healthy, one or more pods running
//	dimmed            healthy, nothing running
//	pulsing bars      stack unreachable — self-healing in progress
//	red ✗             unreachable for over a minute: healing failed
//
// The dropdown: brand header, status line with a coloured dot, one
// row per pod (status dot + domain — name) whose submenu carries the
// actions (Open in browser / Start / Stop / Restart — native menus
// allow one click target per row, so actions live a hover away),
// doctor, quit, and a Cyber5 footer. Built on fyne.io/systray so the
// same code carries to Windows/Linux later.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"fyne.io/systray"
)

// Full brand logos on a translucent white pill so they pop on any
// menu background (light or dark). Rasterised from the official SVGs
// with real alpha via native NSImage rendering, wordmark outlined to
// paths (no font dependency), then auto-cropped and plated — see
// packaging/brand.
//
//go:embed logo-tainer.png
var tainerLogo []byte

//go:embed logo-cyber5.png
var cyber5Logo []byte

const (
	spinnerFrame = 160 * time.Millisecond
	maxPodSlots  = 16 // pre-created menu slots (systray can't remove items)

	tainerCLI = "/opt/tainer/bin/tainer"

	// Brand links go through our own /go/ redirect endpoint rather than
	// carrying UTM params in the URL. The binary knows only a short,
	// permanent path; the server (a SvelteKit /go/[key] route on each
	// site) looks up the key, credits the click, and 302s to the
	// current destination — so traffic can be retargeted (e.g.
	// /showcase -> /studio) without recompiling or redistributing
	// tainer.
	//
	// PARKED (do not forget): server-side click tracking fires inside
	// the /go/ handler, NOT here — PostHog first, then likely
	// Mixpanel/others via additional tracker fns. Nothing to POST from
	// the app for now; these are website links only. The richer data
	// layer (actual product events) is handled elsewhere, not via /go/.
	tainerURL = "https://tainer.dev/go/tainerapp"
	cyber5URL = "https://cyber5.io/go/tainerapp"
)

type appState int

const (
	stateIdle       appState = iota // healthy, no pods running
	stateActive                     // healthy, pods running
	stateRecovering                 // stack unreachable, watchdogs at work
	stateFailed                     // daemon reports healing failed
)

type podInfo struct {
	Name    string
	Domain  string
	Running bool
}

// podSlot is one pre-created dropdown row plus its action submenu.
type podSlot struct {
	row     *systray.MenuItem
	open    *systray.MenuItem
	start   *systray.MenuItem
	stop    *systray.MenuItem
	restart *systray.MenuItem
}

type app struct {
	icons iconSet

	dotGreen, dotAmber, dotRed, dotGrey []byte

	brand  *systray.MenuItem
	status *systray.MenuItem
	slots  []*podSlot
	doctor *systray.MenuItem
	quit   *systray.MenuItem
	maker  *systray.MenuItem

	mu          sync.Mutex // guards state, engine, pods, stopSpinner
	state       appState
	engine      EngineData // last engine state seen
	pods        []podInfo
	stopSpinner chan struct{}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	systray.Run(func() { onReady(ctx) }, cancel)
	// the watch child is reaped by runWatch's cancel path; give it a
	// moment so it is gone before the process exits.
	time.Sleep(200 * time.Millisecond)
}

// afterAction restores the status line an action overwrote (the stream
// emits nothing when the action changed nothing) and cuts any respawn
// backoff short.
func (a *app) afterAction() {
	a.mu.Lock()
	a.applyEngine()
	a.mu.Unlock()
	wakeWatch()
}

func onReady(ctx context.Context) {
	a := &app{
		icons:    buildIconsStyle(os.Getenv("TAINER_MENU_ICON")),
		dotGreen: renderDot(dotGreen),
		dotAmber: renderDot(dotAmber),
		dotRed:   renderDot(dotRed),
		dotGrey:  renderDot(dotGrey),
	}

	a.setBarIcon(a.icons.idle, a.icons.idleTemplate)
	systray.SetTooltip("tainer")

	// brand header: a full-width branded row (white bg, no highlight),
	// click opens tainer.dev
	a.brand = systray.AddMenuItem("", "Open tainer.dev")
	a.brand.SetBrandView(tainerLogo, tainerURL, 22, 0.0)

	// status line: coloured dot + summary
	a.status = systray.AddMenuItem("checking…", "")
	a.status.SetIcon(a.dotGrey)
	a.status.Disable()

	systray.AddSeparator()

	for i := 0; i < maxPodSlots; i++ {
		s := &podSlot{}
		s.row = systray.AddMenuItem("", "")
		s.open = s.row.AddSubMenuItem("Open in browser", "")
		s.restart = s.row.AddSubMenuItem("Restart", "")
		s.stop = s.row.AddSubMenuItem("Stop", "")
		s.start = s.row.AddSubMenuItem("Start", "")
		s.row.Hide()
		a.slots = append(a.slots, s)
		go a.slotClicks(i, s)
	}

	systray.AddSeparator()
	a.doctor = systray.AddMenuItem("Run doctor (check & heal)", "Check and repair the stack")
	a.quit = systray.AddMenuItem("Quit tainer menu", "")

	systray.AddSeparator()
	a.maker = systray.AddMenuItem("", "tainer is a Cyber5 product — cyber5.io")
	a.maker.SetBrandView(cyber5Logo, cyber5URL, 22, 0.0)

	// brand/footer clicks are handled inside their custom views.
	go func() {
		for range a.doctor.ClickedCh {
			a.runDoctorFix()
		}
	}()
	go func() {
		<-a.quit.ClickedCh
		systray.Quit()
	}()

	go func() {
		_ = runWatch(ctx, tainerCLI, a.onEvent)
	}()
}

// setBarIcon routes through SetTemplateIcon for monochrome styles so
// macOS tints them to match the bar.
func (a *app) setBarIcon(b []byte, template bool) {
	if template {
		systray.SetTemplateIcon(b, b)
	} else {
		systray.SetIcon(b)
	}
}

func openURL(u string) {
	_ = exec.Command("open", u).Start()
}

func (a *app) slotClicks(i int, s *podSlot) {
	pod := func() *podInfo {
		a.mu.Lock()
		defer a.mu.Unlock()
		if i < len(a.pods) {
			p := a.pods[i]
			return &p
		}
		return nil
	}
	go func() {
		for range s.open.ClickedCh {
			if p := pod(); p != nil && p.Domain != "" {
				openURL("https://" + p.Domain)
			}
		}
	}()
	action := func(ch chan struct{}, verb string) {
		for range ch {
			if p := pod(); p != nil {
				a.status.SetTitle(verb + "ing " + p.Name + "…")
				name := p.Name
				go func() {
					_ = exec.Command(tainerCLI, verb, name, "--plain").Run()
					a.afterAction()
				}()
			}
		}
	}
	go action(s.start.ClickedCh, "start")
	go action(s.stop.ClickedCh, "stop")
	go action(s.restart.ClickedCh, "restart")
}

// onEvent folds one watch event into the app state and re-renders.
// hello carries the full pod list (and resyncs after any reconnect);
// pod events are deltas for a single row.
func (a *app) onEvent(e Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case e.Hello != nil:
		a.pods = a.pods[:0]
		for _, p := range e.Hello.Pods {
			a.pods = append(a.pods, podFromData(p))
		}
		a.engine = e.Hello.Engine
	case e.Engine != nil:
		a.engine = *e.Engine
	case e.Pod != nil:
		np := podFromData(*e.Pod)
		found := false
		for i := range a.pods {
			if a.pods[i].Name == np.Name {
				a.pods[i] = np
				found = true
				break
			}
		}
		if !found {
			a.pods = append(a.pods, np)
		}
	default:
		return
	}
	// running pods first, then alphabetical
	sort.Slice(a.pods, func(i, j int) bool {
		if a.pods[i].Running != a.pods[j].Running {
			return a.pods[i].Running
		}
		return a.pods[i].Name < a.pods[j].Name
	})
	a.applyEngine()
	a.renderPodSlots()
}

func podFromData(p PodData) podInfo {
	return podInfo{Name: p.Name, Domain: p.Domain, Running: p.State == "running"}
}

// applyEngine renders the bar icon and status line from the last engine
// state and pod table. Caller holds a.mu.
func (a *app) applyEngine() {
	switch a.engine.State {
	case "recovering":
		a.setState(stateRecovering)
		a.status.SetIcon(a.dotAmber)
		a.status.SetTitle("recovering — " + a.engine.Detail)
	case "failed":
		a.setState(stateFailed)
		a.status.SetIcon(a.dotRed)
		a.status.SetTitle("stack unreachable — healing failed")
	default:
		running := 0
		for _, p := range a.pods {
			if p.Running {
				running++
			}
		}
		a.status.SetIcon(a.dotGreen)
		if running > 0 {
			a.setState(stateActive)
			a.status.SetTitle(fmt.Sprintf("healthy — %d pod%s running", running, plural(running)))
		} else {
			a.setState(stateIdle)
			a.status.SetTitle("healthy — no pods running")
		}
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (a *app) renderPodSlots() {
	for i, s := range a.slots {
		if i >= len(a.pods) {
			s.row.Hide()
			continue
		}
		p := a.pods[i]
		title := p.Name
		if p.Domain != "" {
			title = p.Domain + "  —  " + p.Name
		}
		s.row.SetTitle(title)
		if p.Running {
			s.row.SetIcon(a.dotGreen)
			s.open.Show()
			s.restart.Show()
			s.stop.Show()
			s.start.Hide()
		} else {
			s.row.SetIcon(a.dotGrey)
			s.open.Hide()
			s.restart.Hide()
			s.stop.Hide()
			s.start.Show()
		}
		s.row.Show()
	}
}

// setState swaps the menu bar icon, managing the spinner goroutine
// for the recovering state. Caller holds a.mu.
func (a *app) setState(s appState) {
	if s == a.state {
		return
	}
	// stop an active spinner before switching away
	if a.state == stateRecovering && a.stopSpinner != nil {
		close(a.stopSpinner)
		a.stopSpinner = nil
	}
	a.state = s
	switch s {
	case stateIdle:
		a.setBarIcon(a.icons.idle, a.icons.idleTemplate)
	case stateActive:
		a.setBarIcon(a.icons.active, a.icons.activeTemplate)
	case stateFailed:
		a.setBarIcon(a.icons.failed, a.icons.failedTemplate)
	case stateRecovering:
		stop := make(chan struct{})
		a.stopSpinner = stop
		go func() {
			i := 0
			t := time.NewTicker(spinnerFrame)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					a.setBarIcon(a.icons.recover_[i%len(a.icons.recover_)], a.icons.spinnerTemplate)
					i++
				}
			}
		}()
	}
}

// runDoctorFix shells out to the installed tainer binary so the menu
// app never needs elevated logic of its own.
func (a *app) runDoctorFix() {
	a.status.SetTitle("running doctor --fix…")
	go func() {
		cmd := exec.Command(tainerCLI, "doctor", "--fix", "--plain")
		_ = cmd.Run()
		a.afterAction()
	}()
}
