// Package router supervises tainer's two long-lived edge containers
// (caddy + sshpiper). They share a bridge (cs0) with all pod containers;
// UpdateConfig regenerates Caddyfile + sshpiper upstream files after
// every lifecycle event.
package router

import (
	"context"
	"fmt"
	"sort"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

// Container names — match pod.RouterWebName / pod.RouterSSHName exactly.
// Kept here as literals so the router package does not import pod (which
// would create a cycle now that pod/start.go imports router).
const (
	WebContainerName = "tainer-router-web"
	SSHContainerName = "tainer-router-ssh"
)

// PodEndpoint describes one pod from the router's perspective.
// Populated by tainer.pod after engine.Inspect calls and consumed by
// UpdateConfig to render Caddyfile + sshpiper upstream files.
type PodEndpoint struct {
	Pod          string
	Domain       string
	WebIP        string
	HTTPServices []HTTPService
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

// ensureImpl is the implementation seam for Ensure — the actual create
// path lives in router_ensure.go (next task) and uses pkg/tainer/config
// for cert paths. Stubbed here so router.go compiles standalone.
var ensureImpl = func(ctx context.Context, eng *engine.Client, extraHTTPPorts []int) error {
	return fmt.Errorf("router.Ensure not yet wired (see router_ensure.go)")
}
