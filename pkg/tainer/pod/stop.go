package pod

import (
	"context"
	"fmt"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/router"
)

// Stop shuts down every container in the pod (graceful, default 10s
// timeout) and refreshes router config so the now-stopped pod's
// site block is removed from the Caddyfile.
func Stop(ctx context.Context, eng *engine.Client, podName string) error {
	p, err := Get(ctx, eng, podName)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("pod %q not found", podName)
	}
	for _, c := range p.Containers {
		if err := eng.Stop(ctx, c.Name); err != nil {
			return fmt.Errorf("stop %s: %w", c.Name, err)
		}
	}
	all, err := List(ctx, eng)
	if err != nil {
		return err
	}
	endpoints, err := buildEndpoints(ctx, eng, runningOnly(all))
	if err != nil {
		return err
	}
	return router.UpdateConfig(ctx, eng, endpoints)
}

func runningOnly(pods []Pod) []Pod {
	out := make([]Pod, 0, len(pods))
	for _, p := range pods {
		if p.State() == StateRunning {
			out = append(out, p)
		}
	}
	return out
}
