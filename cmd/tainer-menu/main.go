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
// Clicking opens a dropdown listing running pods; clicking a pod
// opens its https:// URL. Built on fyne.io/systray so the same code
// carries to Windows/Linux later.
package main

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"time"

	"fyne.io/systray"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

const (
	pollEvery     = 5 * time.Second
	failedAfter   = 60 * time.Second // unreachable this long → red ✗
	spinnerFrame  = 160 * time.Millisecond
	maxPodSlots   = 16 // pre-created menu slots (systray can't remove items)
	statusTooltip = "tainer"
)

type appState int

const (
	stateIdle       appState = iota // healthy, no pods running
	stateActive                     // healthy, pods running
	stateRecovering                 // stack unreachable, watchdogs at work
	stateFailed                     // unreachable beyond failedAfter
)

type podInfo struct {
	Name   string
	Domain string
	State  string
}

type app struct {
	icons iconSet

	header *systray.MenuItem
	slots  []*systray.MenuItem
	doctor *systray.MenuItem
	quit   *systray.MenuItem

	state       appState
	downSince   time.Time
	pods        []podInfo
	stopSpinner chan struct{}
}

func main() {
	systray.Run(onReady, func() {})
}

func onReady() {
	a := &app{icons: buildIcons()}

	systray.SetIcon(a.icons.idle)
	systray.SetTooltip(statusTooltip)

	a.header = systray.AddMenuItem("tainer — checking…", "")
	a.header.Disable()
	systray.AddSeparator()
	for i := 0; i < maxPodSlots; i++ {
		it := systray.AddMenuItem("", "")
		it.Hide()
		a.slots = append(a.slots, it)
		go a.slotClicks(i, it)
	}
	systray.AddSeparator()
	a.doctor = systray.AddMenuItem("Run doctor --fix", "Check and repair the stack")
	a.quit = systray.AddMenuItem("Quit tainer menu", "")

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

func (a *app) slotClicks(i int, it *systray.MenuItem) {
	for range it.ClickedCh {
		if i < len(a.pods) && a.pods[i].Domain != "" {
			_ = exec.Command("open", "https://"+a.pods[i].Domain).Start()
		}
	}
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

	var running []podInfo
	for _, p := range pods {
		if p.State() != pod.StateRunning {
			continue
		}
		info := podInfo{Name: p.Name, State: string(p.State())}
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
		running = append(running, info)
	}
	sort.Slice(running, func(i, j int) bool { return running[i].Name < running[j].Name })

	a.downSince = time.Time{}
	a.pods = running
	if len(running) > 0 {
		a.setState(stateActive)
		a.header.SetTitle(fmt.Sprintf("tainer — healthy · %d running", len(running)))
	} else {
		a.setState(stateIdle)
		a.header.SetTitle("tainer — healthy · no pods running")
	}
	a.renderPodSlots()
}

func (a *app) setUnreachable(why string) {
	if a.downSince.IsZero() {
		a.downSince = time.Now()
	}
	if time.Since(a.downSince) > failedAfter {
		a.setState(stateFailed)
		a.header.SetTitle("tainer — stack unreachable (self-healing failed)")
	} else {
		a.setState(stateRecovering)
		a.header.SetTitle("tainer — recovering… (" + why + ")")
	}
	a.renderPodSlots()
}

func (a *app) renderPodSlots() {
	for i, it := range a.slots {
		if i < len(a.pods) {
			p := a.pods[i]
			label := p.Name
			if p.Domain != "" {
				label = p.Name + "  →  " + p.Domain
			}
			it.SetTitle(label)
			it.SetTooltip("Open https://" + p.Domain)
			it.Show()
		} else {
			it.Hide()
		}
	}
}

// setState swaps the icon, managing the spinner goroutine for the
// recovering state.
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
		systray.SetIcon(a.icons.idle)
	case stateActive:
		systray.SetIcon(a.icons.active)
	case stateFailed:
		systray.SetIcon(a.icons.failed)
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
					systray.SetIcon(a.icons.recover_[i%len(a.icons.recover_)])
					i++
				}
			}
		}()
	}
}

// runDoctorFix shells out to the installed tainer binary so the menu
// app never needs elevated logic of its own.
func (a *app) runDoctorFix() {
	a.header.SetTitle("tainer — running doctor --fix…")
	go func() {
		cmd := exec.Command("/opt/tainer/bin/tainer", "doctor", "--fix", "--plain")
		_ = cmd.Run()
		a.poll()
	}()
}
