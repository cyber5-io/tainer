package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// stubBringUp swaps the socket/daemon seams for the duration of a test.
func stubBringUp(t *testing.T, dialOK bool) (spawns *int) {
	t.Helper()
	oldDial, oldSpawn, oldWait, oldRefresh, oldHook := dialFn, spawnFn, waitFn, refreshFn, OnReady
	t.Cleanup(func() { dialFn, spawnFn, waitFn, refreshFn, OnReady = oldDial, oldSpawn, oldWait, oldRefresh, oldHook })
	n := 0
	dialFn = func(context.Context, string) (*engine.Client, error) {
		if dialOK {
			return &engine.Client{}, nil
		}
		return nil, errors.New("socket missing")
	}
	spawnFn = func(Options, string) error { n++; return nil }
	waitFn = func(context.Context, string) (*engine.Client, error) { return &engine.Client{}, nil }
	refreshFn = func(context.Context, *engine.Client) {}
	return &n
}

// The fast path is the only path on Linux, where systemd owns the daemon,
// so the ready hook must fire there too.
func TestOnReadyRunsOnFastPath(t *testing.T) {
	spawns := stubBringUp(t, true)
	var got *engine.Client
	calls := 0
	OnReady = func(_ context.Context, cli *engine.Client) { calls++; got = cli }
	cli, err := Engine(context.Background(), Options{SocketPath: "/x.sock"})
	if err != nil {
		t.Fatalf("Engine: %v", err)
	}
	if *spawns != 0 || calls != 1 || got != cli {
		t.Fatalf("fast path: spawns=%d hook calls=%d same client=%v, want 0/1/true", *spawns, calls, got == cli)
	}
}

func TestOnReadyRunsOnceAfterSpawn(t *testing.T) {
	spawns := stubBringUp(t, false)
	var got *engine.Client
	calls := 0
	OnReady = func(_ context.Context, cli *engine.Client) { calls++; got = cli }
	cli, err := Engine(context.Background(), Options{SocketPath: "/x.sock", AutoStart: true})
	if err != nil {
		t.Fatalf("Engine: %v", err)
	}
	if *spawns != 1 || calls != 1 || got != cli {
		t.Fatalf("slow path: spawns=%d hook calls=%d same client=%v, want 1/1/true", *spawns, calls, got == cli)
	}
}

func TestOnReadySkippedWhenDaemonNeverAnswers(t *testing.T) {
	stubBringUp(t, false)
	waitFn = func(context.Context, string) (*engine.Client, error) { return nil, errors.New("timeout") }
	calls := 0
	OnReady = func(context.Context, *engine.Client) { calls++ }
	if _, err := Engine(context.Background(), Options{SocketPath: "/x.sock", AutoStart: true}); err == nil {
		t.Fatal("want error when the daemon never answers")
	}
	if calls != 0 {
		t.Fatalf("hook ran %d times after a failed bring-up", calls)
	}
}
