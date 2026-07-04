// tainer-menu — the menu bar face of the tainer stack.
//
// A tiny systray app that watches the same signals `tainer doctor`
// checks and renders them as the brand mark in the macOS menu bar:
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
	"time"

	"fyne.io/systray"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
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
	pollEvery    = 5 * time.Second
	failedAfter  = 60 * time.Second // unreachable this long → red ✗
	spinnerFrame = 160 * time.Millisecond
	maxPodSlots  = 16 // pre-created menu slots (systray can't remove items)

	tainerCLI = "/opt/tainer/bin/tainer"

	// utm-tagged brand links so cyber5.io analytics can tell menu-app
	// clicks from organic traffic
	tainerURL = "https://tainer.dev/?utm_source=tainer-menu&utm_medium=app&utm_campaign=menubar"
	cyber5URL = "https://cyber5.io/?utm_source=tainer-menu&utm_medium=app&utm_campaign=menubar"
)

type appState int

const (
	stateIdle       appState = iota // healthy, no pods running
	stateActive                     // healthy, pods running
	stateRecovering                 // stack unreachable, watchdogs at work
	stateFailed                     // unreachable beyond failedAfter
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

	state       appState
	downSince   time.Time
	pods        []podInfo
	stopSpinner chan struct{}
}

func main() {
	systray.Run(onReady, func() {})
}

func onReady() {
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
	a.brand.SetBrandView(tainerLogo, tainerURL, 22, 1.0)

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
	a.maker.SetBrandView(cyber5Logo, cyber5URL, 22, 1.0)

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

	go a.pollLoop()
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
		if i < len(a.pods) {
			return &a.pods[i]
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
					a.poll()
				}()
			}
		}
	}
	go action(s.start.ClickedCh, "start")
	go action(s.stop.ClickedCh, "stop")
	go action(s.restart.ClickedCh, "restart")
}

func (a *app) pollLoop() {
	a.poll() // immediate first sample
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for range t.C {
		a.poll()
	}
}

// poll samples the stack exactly the way doctor does: socket ping,
// then a container list through the daemon→agent chain.
func (a *app) poll() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	st := runtime.CurrentStatus(ctx, runtime.Options{})
	if !st.Reachable {
		a.setUnreachable("engine not reachable")
		return
	}
	eng, err := engine.New()
	if err != nil {
		a.setUnreachable("engine: " + err.Error())
		return
	}
	defer eng.Close()

	pods, err := pod.List(ctx, eng)
	if err != nil {
		a.setUnreachable("agent not answering")
		return
	}

	var infos []podInfo
	running := 0
	for _, p := range pods {
		info := podInfo{Name: p.Name, Running: p.State() == pod.StateRunning}
		if info.Running {
			running++
		}
		for _, c := range p.Containers {
			if insp, ierr := eng.Inspect(ctx, c.Name); ierr == nil && insp.Config != nil {
				if mp := insp.Config.Labels[pod.LabelManifestPath]; mp != "" {
					if m, merr := manifest.Load(mp); merr == nil {
						info.Domain = m.Project.Domain
					}
					break
				}
			}
		}
		infos = append(infos, info)
	}
	// running pods first, then alphabetical
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Running != infos[j].Running {
			return infos[i].Running
		}
		return infos[i].Name < infos[j].Name
	})

	a.downSince = time.Time{}
	a.pods = infos
	if running > 0 {
		a.setState(stateActive)
		a.status.SetIcon(a.dotGreen)
		a.status.SetTitle(fmt.Sprintf("healthy — %d pod%s running", running, plural(running)))
	} else {
		a.setState(stateIdle)
		a.status.SetIcon(a.dotGreen)
		a.status.SetTitle("healthy — no pods running")
	}
	a.renderPodSlots()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (a *app) setUnreachable(why string) {
	if a.downSince.IsZero() {
		a.downSince = time.Now()
	}
	if time.Since(a.downSince) > failedAfter {
		a.setState(stateFailed)
		a.status.SetIcon(a.dotRed)
		a.status.SetTitle("stack unreachable — healing failed")
	} else {
		a.setState(stateRecovering)
		a.status.SetIcon(a.dotAmber)
		a.status.SetTitle("recovering — " + why)
	}
	a.renderPodSlots()
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
// for the recovering state.
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
		a.poll()
	}()
}
