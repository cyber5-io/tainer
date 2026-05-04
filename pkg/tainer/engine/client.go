// Package engine wraps the Docker Engine API exposed by cyberstackd over
// its Unix socket. It is the only point in tainer that talks to the
// engine directly — every higher-level package (project, router, network)
// depends on this interface rather than on `exec.Command("docker", ...)`
// or `exec.Command("podman", ...)`.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// DefaultSocketPath is where cyberstackd listens by default. It mirrors
// cmd/cyberstackd/main.go's defaultPath("cyberstack.sock").
func DefaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cyberstack", "cyberstack.sock")
}

// Client is a thin wrapper around the official Docker Go SDK pinned to
// cyberstackd's Unix socket. The wrapper exists so higher layers don't
// import docker/* directly and so the socket location stays a single
// source of truth.
type Client struct {
	api    *client.Client
	socket string
}

// New dials the default cyberstackd socket. Honours DOCKER_HOST when set
// (lets users point tainer at a remote engine for diagnostics).
func New() (*Client, error) {
	socket := DefaultSocketPath()
	if env := os.Getenv("DOCKER_HOST"); env != "" {
		// SDK parses DOCKER_HOST itself; we just record it for error messages.
		socket = env
	} else {
		// Force the SDK onto our socket explicitly so it doesn't pick up
		// whatever stale DOCKER_HOST docker desktop / orbstack may have set.
		_ = os.Setenv("DOCKER_HOST", "unix://"+socket)
	}

	api, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("engine: dial cyberstackd: %w", err)
	}
	return &Client{api: api, socket: socket}, nil
}

// Close releases the underlying HTTP connection pool.
func (c *Client) Close() error {
	if c.api == nil {
		return nil
	}
	return c.api.Close()
}

// Socket returns the path tainer is talking to. Useful for error messages
// and for tainer's diagnostic commands.
func (c *Client) Socket() string { return c.socket }

// Ping verifies cyberstackd is reachable and responds to /v1.x/_ping.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.api.Ping(ctx)
	return err
}

// Pull blocks until the named image is fully resolved on the engine.
// It does not stream progress — callers wanting progress should drive
// the ImagePull stream themselves via the underlying SDK if needed.
func (c *Client) Pull(ctx context.Context, ref string) error {
	body, err := c.api.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("engine: pull %s: %w", ref, err)
	}
	defer body.Close()
	if _, err := io.Copy(io.Discard, body); err != nil {
		return fmt.Errorf("engine: pull %s drain: %w", ref, err)
	}
	return nil
}

// Inspect returns the engine's container record for name (id or name).
func (c *Client) Inspect(ctx context.Context, name string) (container.InspectResponse, error) {
	return c.api.ContainerInspect(ctx, name)
}

// Stop signals the named container to stop. nil if already stopped.
func (c *Client) Stop(ctx context.Context, name string) error {
	if err := c.api.ContainerStop(ctx, name, container.StopOptions{}); err != nil {
		if client.IsErrNotFound(err) {
			return nil
		}
		return fmt.Errorf("engine: stop %s: %w", name, err)
	}
	return nil
}

// Remove deletes the named container. force=true kills if running.
func (c *Client) Remove(ctx context.Context, name string, force bool) error {
	err := c.api.ContainerRemove(ctx, name, container.RemoveOptions{Force: force})
	if err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("engine: remove %s: %w", name, err)
	}
	return nil
}

// Logs returns a reader over the container's combined stdout+stderr.
// follow=true streams new entries as they arrive; the caller closes.
func (c *Client) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	return c.api.ContainerLogs(ctx, name, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
	})
}

// LabelFilter builds a Docker label filter that matches "<key>=<value>"
// when value is non-empty, or "<key>" (key existence) when value is "".
// Returns a filters.Args ready to pass into ContainerList/NetworkList.
func LabelFilter(key, value string) filters.Args {
	args := filters.NewArgs()
	if value == "" {
		args.Add("label", key)
	} else {
		args.Add("label", key+"="+value)
	}
	return args
}

// ContainerList exposes the underlying SDK so pod-listing callers can
// pass their own label filters. Returns the raw SDK type.
func (c *Client) ContainerList(ctx context.Context, opts container.ListOptions) ([]container.Summary, error) {
	return c.api.ContainerList(ctx, opts)
}

// NetworkExists checks for a user-defined bridge network by name.
func (c *Client) NetworkExists(ctx context.Context, name string) (bool, error) {
	_, err := c.api.NetworkInspect(ctx, name, network.InspectOptions{})
	if err == nil {
		return true, nil
	}
	if client.IsErrNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("engine: network exists %s: %w", name, err)
}

// NetworkCreate creates a user-defined bridge network with the given
// subnet (CIDR). Idempotent: returns nil if a network with that name
// already exists.
func (c *Client) NetworkCreate(ctx context.Context, name, subnet string) error {
	exists, err := c.NetworkExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	opts := network.CreateOptions{
		Driver: "bridge",
	}
	if subnet != "" {
		opts.IPAM = &network.IPAM{Config: []network.IPAMConfig{{Subnet: subnet}}}
	}
	if _, err := c.api.NetworkCreate(ctx, name, opts); err != nil {
		return fmt.Errorf("engine: network create %s: %w", name, err)
	}
	return nil
}

// NetworkRemove deletes a user-defined network. Idempotent.
func (c *Client) NetworkRemove(ctx context.Context, name string) error {
	err := c.api.NetworkRemove(ctx, name)
	if err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("engine: network remove %s: %w", name, err)
	}
	return nil
}

// NetworkConnect attaches a container to a network. Idempotent: returns
// nil when the container is already attached.
func (c *Client) NetworkConnect(ctx context.Context, networkName, containerName string) error {
	err := c.api.NetworkConnect(ctx, networkName, containerName, nil)
	if err == nil {
		return nil
	}
	// The engine returns 403 with "already exists" when re-attaching;
	// treat as success.
	if strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return fmt.Errorf("engine: network connect %s -> %s: %w", containerName, networkName, err)
}

// NetworkDisconnect detaches a container from a network. Idempotent:
// nil if the container or network is missing, or if not connected.
func (c *Client) NetworkDisconnect(ctx context.Context, networkName, containerName string) error {
	err := c.api.NetworkDisconnect(ctx, networkName, containerName, true)
	if err == nil || client.IsErrNotFound(err) {
		return nil
	}
	if strings.Contains(err.Error(), "not connected") {
		return nil
	}
	return fmt.Errorf("engine: network disconnect %s <- %s: %w", containerName, networkName, err)
}

// NetworkSubnets returns every subnet currently in use across all
// user-defined networks. Used by tainer to pick a free /24 when
// scaffolding a new project.
func (c *Client) NetworkSubnets(ctx context.Context) ([]string, error) {
	nets, err := c.api.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("engine: network list: %w", err)
	}
	var out []string
	for _, n := range nets {
		for _, cfg := range n.IPAM.Config {
			if cfg.Subnet != "" {
				out = append(out, cfg.Subnet)
			}
		}
	}
	return out, nil
}

// ErrNotFound is returned by helpers that distinguish "missing" from
// "engine error". The underlying SDK uses an opaque error type; this
// is an explicit sentinel for callers that prefer errors.Is.
var ErrNotFound = errors.New("not found")
