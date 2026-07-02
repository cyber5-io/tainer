// Package networkcmd implements `tainer network mode show|set`. Lives
// outside pkg/tainer/network to keep the network package free of CLI
// concerns (the lifecycle code only ever needs Mode + ReadMode/WriteMode).
package networkcmd

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/network"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// Current reads and returns the active network mode. Wraps
// network.ReadMode + DefaultModeFile so the CLI doesn't have to know
// where the persistence file lives.
func Current() (network.Mode, error) {
	return network.ReadMode(network.DefaultModeFile())
}

// SwitchResult is what Switch returns on success — pure data, no I/O.
// CLI renders it via the brand surface (styled / plain / json).
type SwitchResult struct {
	Previous  network.Mode
	New       network.Mode
	NoChange  bool     // true when Previous == New (no-op)
	Restarted []string // pods that were running before and came back after
	Failed    []string // pods we couldn't restart (already-stopped pods aren't here — they weren't running to begin with)
}

// Switch swaps the daemon's network mode and restarts any pods that
// were running before. Behaviour mirrors the old Set:
//
//  1. List currently-running pods
//  2. Stop them
//  3. Stop cyberstackd
//  4. Persist the new mode
//  5. Spawn cyberstackd (transparent bring-up reads the new mode)
//  6. Restart the previously-running pods
//
// On failure to spawn the new daemon, the mode file is rolled back.
// Pure data-only return — the CLI is responsible for rendering.
func Switch(ctx context.Context, raw string) (*SwitchResult, error) {
	want, err := network.ParseMode(raw)
	if err != nil {
		return nil, err
	}
	current, err := network.ReadMode(network.DefaultModeFile())
	if err != nil {
		return nil, err
	}
	res := &SwitchResult{Previous: current, New: want}
	if current == want {
		res.NoChange = true
		return res, nil
	}

	// 1. Capture currently-running pods.
	eng, err := engine.New()
	if err != nil {
		return res, err
	}
	defer eng.Close()
	pods, err := pod.List(ctx, eng)
	if err != nil {
		return res, err
	}
	var running []string
	for _, p := range pods {
		if p.State() == pod.StateRunning {
			running = append(running, p.Name)
		}
	}

	// 2. Stop them.
	for _, name := range running {
		if err := pod.Stop(ctx, eng, name); err != nil {
			fmt.Fprintf(os.Stderr, "warning: stop %s: %v\n", name, err)
		}
	}

	// 3. Stop cyberstackd (relies on Step 7's transparent bring-up to
	//    spawn it back with the new mode on next runtime.Engine call).
	_ = eng.Close()
	if err := stopDaemon(); err != nil {
		return res, fmt.Errorf("stop cyberstackd: %w", err)
	}

	// 4. Persist new mode.
	if err := network.WriteMode(network.DefaultModeFile(), want); err != nil {
		return res, err
	}

	// 5. Spawn daemon in new mode + restart pods.
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	eng2, err := runtime.Engine(ctx2, runtime.Options{AutoStart: true, NetworkMode: want})
	if err != nil {
		// Roll back.
		_ = network.WriteMode(network.DefaultModeFile(), current)
		return res, fmt.Errorf("start cyberstackd in %s: %w (reverted to %s)", want, err, current)
	}
	defer eng2.Close()

	for _, name := range running {
		// We rely on the pod's manifest-path label to locate the manifest.
		manifestPath, ok := manifestPathFor(ctx2, eng2, name)
		if !ok {
			res.Failed = append(res.Failed, name+" (manifest path unknown)")
			continue
		}
		if _, err := pod.Start(ctx2, eng2, pod.StartOptions{ManifestPath: manifestPath}); err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		res.Restarted = append(res.Restarted, name)
	}
	return res, nil
}

// manifestPathFor reads the tainer.manifest-path label from any of the
// pod's containers.
func manifestPathFor(ctx context.Context, eng *engine.Client, podName string) (string, bool) {
	pods, err := pod.List(ctx, eng)
	if err != nil {
		return "", false
	}
	for _, p := range pods {
		if p.Name != podName {
			continue
		}
		for _, c := range p.Containers {
			insp, err := eng.Inspect(ctx, c.Name)
			if err == nil {
				if path := insp.Config.Labels[pod.LabelManifestPath]; path != "" {
					return path, true
				}
			}
		}
	}
	return "", false
}

// stopDaemon sends SIGTERM to cyberstackd. The 0.4 shutdown handler
// drains in-flight requests and exits cleanly. SIGKILL after 30s.
func stopDaemon() error {
	pid := runtime.ReadPID(engine.DefaultSocketPath())
	if pid == 0 {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return nil // process gone
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return nil
}
