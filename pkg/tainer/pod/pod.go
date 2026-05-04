package pod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/docker/docker/api/types/container"
)

// Pod is the runtime view of a tainer project assembled by listing
// `tainer.pod=<name>` containers from the engine and grouping them.
type Pod struct {
	Name         string
	SubnetOctet  int
	Containers   []Container
	ManifestHash string
}

// Container is one container inside a Pod.
type Container struct {
	ID            string
	Role          string
	Name          string
	State         string
	IP            string
	PublishedPort int
}

// State buckets a Pod's mixed container states into a single label.
type State string

const (
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateMixed   State = "mixed"
)

func (p Pod) State() State {
	if len(p.Containers) == 0 {
		return StateStopped
	}
	running := 0
	for _, c := range p.Containers {
		if c.State == "running" {
			running++
		}
	}
	switch running {
	case 0:
		return StateStopped
	case len(p.Containers):
		return StateRunning
	default:
		return StateMixed
	}
}

// List returns every pod the engine knows about, regardless of state.
func List(ctx context.Context, eng *engine.Client) ([]Pod, error) {
	containers, err := eng.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: engine.LabelFilter(LabelPod, ""),
	})
	if err != nil {
		return nil, fmt.Errorf("pod: list: %w", err)
	}
	byPod := map[string]*Pod{}
	for _, c := range containers {
		name := c.Labels[LabelPod]
		if name == "" {
			continue
		}
		p, ok := byPod[name]
		if !ok {
			octet, _ := strconv.Atoi(c.Labels[LabelSubnetOctet])
			p = &Pod{Name: name, SubnetOctet: octet, ManifestHash: c.Labels[LabelManifestHash]}
			byPod[name] = p
		}
		published := 0
		for k, v := range c.Labels {
			if strings.HasPrefix(k, LabelPublishPrefix) && strings.TrimPrefix(k, LabelPublishPrefix) == c.Labels[LabelRole] {
				published, _ = strconv.Atoi(v)
			}
		}
		nm := ""
		if len(c.Names) > 0 {
			nm = strings.TrimPrefix(c.Names[0], "/")
		}
		p.Containers = append(p.Containers, Container{
			ID:            c.ID,
			Role:          c.Labels[LabelRole],
			Name:          nm,
			State:         c.State,
			PublishedPort: published,
		})
	}
	out := make([]Pod, 0, len(byPod))
	for _, p := range byPod {
		sort.Slice(p.Containers, func(i, j int) bool { return p.Containers[i].Role < p.Containers[j].Role })
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns a single Pod by name, or nil if unknown.
func Get(ctx context.Context, eng *engine.Client, name string) (*Pod, error) {
	all, err := List(ctx, eng)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, nil
}

// ManifestHash returns a stable SHA over the resolved manifest. Used
// to label every container so we can detect drift between manifest
// and running state.
func ManifestHash(m *manifest.Manifest) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s/%s/%s/%s\n", m.Project.Name, m.Project.Type, m.Project.Domain, m.Pod.Size)
	fmt.Fprintf(h, "php=%s node=%s db=%s\n", m.Runtime.PHP, m.Runtime.Node, m.Runtime.Database)
	for _, p := range m.Ports {
		fmt.Fprintf(h, "port %d/%s\n", p.Port, p.Protocol)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
