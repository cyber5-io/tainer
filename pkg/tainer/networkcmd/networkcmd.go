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

// Show prints the current mode and the active vfkit interface (if
// detectable) to stdout.
func Show() error {
	mode, err := network.ReadMode(network.DefaultModeFile())
	if err != nil {
		return err
	}
	fmt.Printf("network mode: %s\n", mode.Display())
	return nil
}

// Set switches the daemon to the requested mode, auto-restarting any
// running pods. See spec §"Network mode UX" for the flow.
func Set(ctx context.Context, raw string) error {
	want, err := network.ParseMode(raw)
	if err != nil {
		return err
	}
	current, err := network.ReadMode(network.DefaultModeFile())
	if err != nil {
		return err
	}
	if current == want {
		fmt.Printf("network mode already %s\n", want.Display())
		return nil
	}

	// 1. Capture currently-running pods.
	eng, err := engine.New()
	if err != nil {
		return err
	}
	defer eng.Close()
	pods, err := pod.List(ctx, eng)
	if err != nil {
		return err
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
		return fmt.Errorf("stop cyberstackd: %w", err)
	}

	// 4. Persist new mode.
	if err := network.WriteMode(network.DefaultModeFile(), want); err != nil {
		return err
	}

	// 5. Spawn daemon in new mode + restart pods.
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	eng2, err := runtime.Engine(ctx2, runtime.Options{AutoStart: true, NetworkMode: want})
	if err != nil {
		// Roll back.
		_ = network.WriteMode(network.DefaultModeFile(), current)
		return fmt.Errorf("start cyberstackd in %s: %w (reverted to %s)", want, err, current)
	}
	defer eng2.Close()

	var failed []string
	for _, name := range running {
		// We rely on the pod's manifest-path label to locate the manifest.
		manifestPath, ok := manifestPathFor(ctx2, eng2, name)
		if !ok {
			failed = append(failed, name+" (manifest path unknown)")
			continue
		}
		if _, err := pod.Start(ctx2, eng2, pod.StartOptions{ManifestPath: manifestPath}); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", name, err))
		}
	}
	fmt.Printf("Switched to %s.\n", want.Display())
	if len(running) > 0 {
		fmt.Printf("  Restarted: %v\n", running)
	}
	if len(failed) > 0 {
		fmt.Printf("  Failed:    %v\n", failed)
	}
	return nil
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
