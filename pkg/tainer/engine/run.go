package engine

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// PortMap describes a host:container port publish on a single proto.
type PortMap struct {
	Container int    // port number inside the container
	Host      int    // port number to publish on the host (0 = random)
	Proto     string // "tcp" (default) or "udp"
	Role      string // tainer role this binding belongs to (e.g. "db", "app"); informational only — not sent to docker
}

// Mount describes a bind- or volume-mount for a container.
//
//	Source = host path or volume name
//	Target = absolute path inside the container
//	ReadOnly mounts the source RO; otherwise RW.
//	Volume=true treats Source as a named volume; otherwise bind.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
	Volume   bool
}

// RunSpec is the union of options Project orchestration needs from the
// engine. It deliberately omits options the rebuild does not exercise
// (cgroup parents, ulimits, capabilities, etc.) — add as needed, not
// speculatively.
type RunSpec struct {
	Image       string
	Name        string
	Network     string   // legacy: user-defined bridge to attach to (kept for now, unused by tainer 0.9.x)
	NetworkMode string   // "" = default bridge (own veth on cs0); "container:<id-or-name>" = shared netns
	Env         []string // KEY=VALUE form
	Cmd         []string
	Mounts      []Mount
	Ports       []PortMap
	Detach      bool // true = create+start detached; false = create only (caller starts)
	Restart     string
	Resources   container.Resources // memory + NanoCPUs
	Labels      map[string]string
	// Smart pods: all of a pod's containers share one cgroup parent that
	// carries the pod's aggregate budget (Resources above conveys it; the
	// engine applies it to the parent, not this container).
	CgroupParent string
	OomScoreAdj  int64 // OOM victim bias within the shared budget (db negative)
}

// Run creates a container per spec and (if Detach) starts it.
// Returns the container ID assigned by the engine.
func (c *Client) Run(ctx context.Context, spec RunSpec) (string, error) {
	cfg := &container.Config{
		Image: spec.Image,
		Env:   spec.Env,
		Cmd:   spec.Cmd,
	}
	cfg.Labels = spec.Labels
	hostCfg := &container.HostConfig{}
	hostCfg.Resources = spec.Resources
	if spec.CgroupParent != "" {
		hostCfg.CgroupParent = spec.CgroupParent
		hostCfg.OomScoreAdj = int(spec.OomScoreAdj)
	}
	if spec.Restart != "" {
		hostCfg.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(spec.Restart)}
	}

	for _, m := range spec.Mounts {
		mt := mount.Mount{
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		}
		if m.Volume {
			mt.Type = mount.TypeVolume
		} else {
			mt.Type = mount.TypeBind
		}
		hostCfg.Mounts = append(hostCfg.Mounts, mt)
	}

	if len(spec.Ports) > 0 {
		exposed := nat.PortSet{}
		bindings := nat.PortMap{}
		for _, p := range spec.Ports {
			proto := p.Proto
			if proto == "" {
				proto = "tcp"
			}
			port, err := nat.NewPort(proto, fmt.Sprintf("%d", p.Container))
			if err != nil {
				return "", fmt.Errorf("engine: invalid port %d/%s: %w", p.Container, proto, err)
			}
			exposed[port] = struct{}{}
			hostStr := ""
			if p.Host != 0 {
				hostStr = fmt.Sprintf("%d", p.Host)
			}
			bindings[port] = []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: hostStr}}
		}
		cfg.ExposedPorts = exposed
		hostCfg.PortBindings = bindings
	}

	if spec.NetworkMode != "" {
		hostCfg.NetworkMode = container.NetworkMode(spec.NetworkMode)
	}

	var netCfg *network.NetworkingConfig
	if spec.Network != "" {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				spec.Network: {NetworkID: spec.Network},
			},
		}
	}

	resp, err := c.api.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("engine: create %s: %w", spec.Name, err)
	}

	if spec.Detach {
		if err := c.api.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			return resp.ID, fmt.Errorf("engine: start %s: %w", spec.Name, err)
		}
	}
	return resp.ID, nil
}

// Start starts a created container by name or ID.
func (c *Client) Start(ctx context.Context, name string) error {
	if err := c.api.ContainerStart(ctx, name, container.StartOptions{}); err != nil {
		return fmt.Errorf("engine: start %s: %w", name, err)
	}
	return nil
}
