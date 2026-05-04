// Package router supervises tainer's two long-lived edge containers
// (caddy + sshpiper). They live outside the pod abstraction — each
// pod's lifecycle attaches/detaches them from the pod's network so
// the router can reach project containers. UpdateConfig regenerates
// Caddyfile + sshpiper upstream files after every lifecycle event.
package router

import (
	"context"
	"fmt"
	"sort"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
)

// Container names — generic role-based, same convention as pods.
// Aliased from pod.* so the dependency direction stays one-way.
const (
	WebContainerName = pod.RouterWebName
	SSHContainerName = pod.RouterSSHName
)

// PodEndpoint describes one pod from the router's perspective.
// Populated by tainer.pod after engine.Inspect calls and consumed by
// UpdateConfig to render Caddyfile + sshpiper upstream files.
type PodEndpoint struct {
	Pod          string
	Domain       string
	WebIP        string
	HTTPServices []HTTPService
	SSHIP        string // app/web container IP — sshpiper forwards here
}

// HTTPService is one extra HTTP-protocol port declared by a pod's
// manifest. Routed via the edge caddy on the project domain.
type HTTPService struct {
	Role string
	Port int
	IP   string // container IP serving this port on the pod network
}

// ExtraHTTPPorts is the deduped sorted set of host ports the edge
// caddy must bind, derived from every pod's HTTPServices. Used by
// Ensure to decide whether the router caddy needs recreating with
// additional `-p` bindings.
func ExtraHTTPPorts(pods []PodEndpoint) []int {
	seen := map[int]struct{}{}
	for _, p := range pods {
		for _, h := range p.HTTPServices {
			seen[h.Port] = struct{}{}
		}
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// Ensure makes sure both router containers exist and are running with
// the correct port bindings. Recreates the caddy container if extra
// HTTP ports have changed (Docker doesn't allow adding bindings to a
// running container).
func Ensure(ctx context.Context, eng *engine.Client, extraHTTPPorts []int) error {
	return ensureImpl(ctx, eng, extraHTTPPorts)
}

// AttachToPod connects both router containers to the pod's network.
// Idempotent (engine.NetworkConnect handles "already connected").
func AttachToPod(ctx context.Context, eng *engine.Client, podName string) error {
	netName := pod.NetworkName(podName)
	if err := eng.NetworkConnect(ctx, netName, WebContainerName); err != nil {
		return fmt.Errorf("router attach web: %w", err)
	}
	if err := eng.NetworkConnect(ctx, netName, SSHContainerName); err != nil {
		return fmt.Errorf("router attach ssh: %w", err)
	}
	return nil
}

// DetachFromPod disconnects both router containers from the pod's
// network. Idempotent.
func DetachFromPod(ctx context.Context, eng *engine.Client, podName string) error {
	netName := pod.NetworkName(podName)
	if err := eng.NetworkDisconnect(ctx, netName, WebContainerName); err != nil {
		return fmt.Errorf("router detach web: %w", err)
	}
	if err := eng.NetworkDisconnect(ctx, netName, SSHContainerName); err != nil {
		return fmt.Errorf("router detach ssh: %w", err)
	}
	return nil
}

// ensureImpl is the implementation seam for Ensure — the actual create
// path lives in router_ensure.go (next task) and uses pkg/tainer/config
// for cert paths. Stubbed here so router.go compiles standalone.
var ensureImpl = func(ctx context.Context, eng *engine.Client, extraHTTPPorts []int) error {
	return fmt.Errorf("router.Ensure not yet wired (see router_ensure.go)")
}
