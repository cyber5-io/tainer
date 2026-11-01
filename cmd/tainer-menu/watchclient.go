package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// Wire shapes of the `tainer watch --json` event stream
// (docs/api/cli-json-v1.md section 3).

type EngineData struct {
	State  string `json:"state"` // healthy | recovering | failed
	Since  string `json:"since"`
	Detail string `json:"detail"`
}

type PodData struct {
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	State   string `json:"state"` // running | stopped | partial
	Running int    `json:"running"`
	Total   int    `json:"total"`
}

type HelloData struct {
	Engine EngineData `json:"engine"`
	Pods   []PodData  `json:"pods"`
}

// Event is one decoded envelope. Exactly one of Hello/Engine/Pod is set
// for the families the menu understands; other types (progress, future
// ones) arrive with only Type and TS populated.
type Event struct {
	V      int             `json:"v"`
	TS     string          `json:"ts"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
	Hello  *HelloData      `json:"-"`
	Engine *EngineData     `json:"-"`
	Pod    *PodData        `json:"-"`
}

const backoffCap = 30 * time.Second

// backoffBase is the first respawn delay; it doubles up to backoffCap.
var backoffBase = time.Second

func decodeEvent(line []byte) (Event, bool) {
	var e Event
	if err := json.Unmarshal(line, &e); err != nil || e.Type == "" {
		// truncated final line, error envelope, or noise
		return Event{}, false
	}
	var err error
	switch e.Type {
	case "hello":
		e.Hello = &HelloData{}
		err = json.Unmarshal(e.Data, e.Hello)
	case "engine":
		e.Engine = &EngineData{}
		err = json.Unmarshal(e.Data, e.Engine)
	case "pod":
		e.Pod = &PodData{}
		err = json.Unmarshal(e.Data, e.Pod)
	}
	if err != nil {
		return Event{}, false
	}
	return e, true
}

func recoveringEvent(detail string) Event {
	return Event{
		V:      1,
		TS:     time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Type:   "engine",
		Engine: &EngineData{State: "recovering", Detail: detail},
	}
}

// runWatch keeps `<cli> watch --json` running and feeds its events to
// onEvent until ctx is cancelled. Whenever the process ends (daemon
// restart, watch-ended, signal exit, spawn failure) it reports an engine
// recovering event, waits with exponential backoff and respawns; a
// stream that delivered a hello resets the backoff. It returns only
// ctx's error.
func runWatch(ctx context.Context, cli string, onEvent func(Event)) error {
	delay := backoffBase
	for {
		gotHello, detail := watchOnce(ctx, cli, onEvent)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if gotHello {
			delay = backoffBase
		}
		onEvent(recoveringEvent(detail))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay *= 2; delay > backoffCap {
			delay = backoffCap
		}
	}
}

// watchOnce runs one subprocess to completion.
func watchOnce(ctx context.Context, cli string, onEvent func(Event)) (gotHello bool, detail string) {
	cmd := exec.CommandContext(ctx, cli, "watch", "--json")
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false, "engine not reachable"
	}
	if err := cmd.Start(); err != nil {
		return false, "engine not reachable"
	}
	r := bufio.NewReaderSize(out, 64*1024)
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 {
			if e, ok := decodeEvent(line); ok {
				if e.Type == "hello" {
					gotHello = true
				}
				onEvent(e)
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				_ = cmd.Process.Kill()
			}
			break
		}
	}
	_ = cmd.Wait()
	return gotHello, "reconnecting to the engine"
}
