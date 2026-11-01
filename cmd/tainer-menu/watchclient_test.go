package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fakeCLI(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-tainer")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWatchClientDecodesAndDispatches(t *testing.T) {
	cli := fakeCLI(t, `[ "$1" = watch ] && [ "$2" = --json ] || exit 2
echo '{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"hello","data":{"engine":{"state":"healthy","since":"2026-10-31T12:00:00.000Z","detail":"ok"},"pods":[{"name":"blog","domain":"blog.tainer.me","state":"partial","running":1,"total":2}]}}'
echo '{"v":1,"ts":"2026-10-31T12:00:01.000Z","type":"pod","data":{"name":"blog","domain":"blog.tainer.me","state":"running","running":2,"total":2}}'
echo '{"v":1,"ts":"2026-10-31T12:00:01.000Z","type":"mystery","data":{}}'
printf '{"v":1,"ts":"trunc'
sleep 30
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var got []Event
	done := make(chan struct{})
	go func() {
		_ = runWatch(ctx, cli, func(e Event) {
			mu.Lock()
			got = append(got, e)
			n := len(got)
			mu.Unlock()
			if n == 3 {
				close(done)
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for events")
	}
	cancel()
	mu.Lock()
	defer mu.Unlock()
	if got[0].Type != "hello" || got[0].Hello == nil || got[0].Hello.Engine.State != "healthy" ||
		len(got[0].Hello.Pods) != 1 || got[0].Hello.Pods[0].Name != "blog" || got[0].Hello.Pods[0].Running != 1 {
		t.Fatalf("bad hello: %+v", got[0])
	}
	if got[1].Type != "pod" || got[1].Pod == nil || got[1].Pod.State != "running" || got[1].Pod.Total != 2 {
		t.Fatalf("bad pod: %+v", got[1])
	}
	if got[2].Type != "mystery" {
		t.Fatalf("unknown types must pass through: %+v", got[2])
	}
}

func TestWatchClientRespawnBackoff(t *testing.T) {
	old := backoffBase
	backoffBase = 150 * time.Millisecond
	defer func() { backoffBase = old }()

	counter := filepath.Join(t.TempDir(), "spawns")
	cli := fakeCLI(t, "echo spawn >> "+counter+"\nexit 1\n")
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	var mu sync.Mutex
	var recovering int
	err := runWatch(ctx, cli, func(e Event) {
		if e.Type == "engine" && e.Engine != nil && e.Engine.State == "recovering" {
			mu.Lock()
			recovering++
			mu.Unlock()
		}
	})
	if err == nil {
		t.Fatal("expected context error")
	}
	b, _ := os.ReadFile(counter)
	spawns := len(strings.Fields(string(b)))
	// 150ms, 300ms backoffs inside a 700ms window: more than one spawn
	// (respawn happened) but far fewer than a hot loop.
	if spawns < 2 || spawns > 4 {
		t.Fatalf("want 2-4 spawns with backoff, got %d", spawns)
	}
	mu.Lock()
	defer mu.Unlock()
	if recovering < 2 {
		t.Fatalf("gap must surface recovering state, got %d", recovering)
	}
}
