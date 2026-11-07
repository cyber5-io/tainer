package imagecheck

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

const maxConcurrency = 6

// Deps are the outside-world lookups Run needs; tests fake them all.
type Deps struct {
	EnginePods         func(ctx context.Context) ([]pod.Pod, error)
	ImageInspect       func(ctx context.Context, ref string) (engine.ImageInfo, error)
	RemoteConfigDigest func(ctx context.Context, ref Ref, arch string) (string, error)
	// RemoteLabels reads an image's labels from the registry (control pod).
	RemoteLabels func(ctx context.Context, ref Ref, arch string) (map[string]string, error)
	// TainerVersion is this tainer's release version; "dev" or empty for a
	// development build.
	TainerVersion string
}

// Report is the `tainer update --check --json` document. The envelope's
// "v" key is added by the JSON writer.
type Report struct {
	CheckedAt string      `json:"checkedAt"`
	Pods      []PodReport `json:"pods"`
	UpToDate  int         `json:"upToDate"`
	Available int         `json:"available"`
	Unknown   int         `json:"unknown"`
	// Held counts pods whose newer image needs a newer tainer.
	Held int `json:"held,omitempty"`
}

type PodReport struct {
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	Domain          string       `json:"domain"`
	State           string       `json:"state"`
	UpdateAvailable bool         `json:"updateAvailable"`
	Parts           string       `json:"parts"`
	Roles           []RoleReport `json:"roles"`
	// Control is set for the control pod: its version against the stable
	// channel.
	Control *ControlCheck `json:"control,omitempty"`
	Error   string        `json:"error,omitempty"`
}

type RoleReport struct {
	Role            string `json:"role"`
	Image           string `json:"image"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

type refResult struct {
	update bool
	err    error
}

type podPlan struct {
	name  string
	m     *manifest.Manifest
	state string
	roles []RoleReport
	err   error
	// control is set for the pinned control pod: the version it runs. Its
	// app role is judged by version against the stable channel, not by
	// digest.
	control string
}

// Run compares each registered project's local images with the registry,
// metadata only. Projects with no engine pod are reported as stopped and
// up to date.
func Run(ctx context.Context, d Deps, projects map[string]registry.Project) (Report, error) {
	pods, err := d.EnginePods(ctx)
	if err != nil {
		return Report{}, err
	}
	byName := make(map[string]pod.Pod, len(pods))
	for _, p := range pods {
		byName[p.Name] = p
	}

	names := make([]string, 0, len(projects))
	for n := range projects {
		names = append(names, n)
	}
	sort.Strings(names)

	plans := make([]podPlan, 0, len(names))
	unique := map[string]struct{}{}
	for _, n := range names {
		ep, hasPod := byName[n]
		pl := podPlan{name: n, state: "running"}
		if !hasPod || ep.State() == pod.StateStopped {
			pl.state = "stopped"
		}
		m, err := manifest.Load(filepath.Join(projects[n].Path, manifest.FileName))
		if err != nil {
			pl.err = fmt.Errorf("manifest: %w", err)
			plans = append(plans, pl)
			continue
		}
		pl.m = m
		if !hasPod {
			// No containers (never started, or updated while stopped): the
			// next start pulls fresh parts, so there is nothing to compare.
			plans = append(plans, pl)
			continue
		}
		// Only the built-in pod in its own folder is the dashboard; a user's
		// project that happens to be named control is checked like any other.
		pinned := tainer.IsPinnedProject(n) && projects[n].Path == config.ControlDir()
		if pinned {
			pl.control = runningControlVersion(ep, projects[n].ImageVersion)
		}
		for _, role := range pod.RolesForPod(m) {
			if pinned && role == pod.RoleApp {
				pl.roles = append(pl.roles, RoleReport{Role: role, Image: pod.ControlImageRef(pl.control)})
				continue
			}
			ref := pod.ImageRef(m, role)
			if ref == "" {
				continue
			}
			pl.roles = append(pl.roles, RoleReport{Role: role, Image: ref})
			unique[ref] = struct{}{}
		}
		plans = append(plans, pl)
	}

	results := checkRefs(ctx, d, unique)
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}

	rep := Report{CheckedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"), Pods: make([]PodReport, 0, len(plans))}
	for _, pl := range plans {
		pr := PodReport{Name: pl.name, State: pl.state, Roles: []RoleReport{}}
		if pl.m != nil {
			pr.Type = string(pl.m.Project.Type)
			pr.Domain = pl.m.Project.Domain
			pr.Parts = PartsLine(pl.m)
		} else {
			pr.Type = projects[pl.name].Type
			pr.Domain = projects[pl.name].Domain
		}
		var errs []string
		if pl.err != nil {
			errs = append(errs, pl.err.Error())
		}
		if pl.control != "" {
			pr.Control = &ControlCheck{Running: pl.control, State: ControlUnknown}
			if c, err := CheckControl(ctx, d, pl.control); err != nil {
				errs = append(errs, err.Error())
			} else {
				pr.Control = &c
			}
		}
		for _, r := range pl.roles {
			if pr.Control != nil && r.Role == pod.RoleApp {
				r.UpdateAvailable = pr.Control.State == ControlUpdate
				if r.UpdateAvailable {
					pr.UpdateAvailable = true
				}
				pr.Roles = append(pr.Roles, r)
				continue
			}
			res := results[r.Image]
			if res.err != nil {
				errs = append(errs, res.err.Error())
			}
			r.UpdateAvailable = res.err == nil && res.update
			if r.UpdateAvailable {
				pr.UpdateAvailable = true
			}
			pr.Roles = append(pr.Roles, r)
		}
		pr.Error = strings.Join(errs, "; ")
		switch {
		case pr.UpdateAvailable:
			rep.Available++
		case pr.Control != nil && pr.Control.State == ControlNeedsNewerTainer:
			rep.Held++
		case pr.Error != "":
			rep.Unknown++
		default:
			rep.UpToDate++
		}
		rep.Pods = append(rep.Pods, pr)
	}
	return rep, nil
}

// runningControlVersion is the control image version the pod's app actually
// runs, read from its image tag. Without a version tag to read (no app
// container, a channel tag) it is the version the registry records.
func runningControlVersion(p pod.Pod, recorded string) string {
	for _, c := range p.Containers {
		if c.Role != pod.RoleApp {
			continue
		}
		if i := strings.LastIndex(c.Image, ":"); i >= 0 && !strings.Contains(c.Image[i:], "/") {
			if tag := c.Image[i+1:]; semverOf(tag) != "" {
				return trimV(tag)
			}
		}
	}
	return pod.ControlVersion(recorded)
}

func checkRefs(ctx context.Context, d Deps, refs map[string]struct{}) map[string]refResult {
	out := make(map[string]refResult, len(refs))
	var mu sync.Mutex
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for ref := range refs {
		wg.Add(1)
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Done()
			continue
		}
		go func(ref string) {
			defer wg.Done()
			defer func() { <-sem }()
			res := checkRef(ctx, d, ref)
			mu.Lock()
			out[ref] = res
			mu.Unlock()
		}(ref)
	}
	wg.Wait()
	return out
}

func checkRef(ctx context.Context, d Deps, ref string) refResult {
	parsed, err := ParseRef(ref)
	if err != nil {
		return refResult{err: err}
	}
	local, err := d.ImageInspect(ctx, ref)
	if err != nil {
		return refResult{err: fmt.Errorf("local %s: %w", ref, err)}
	}
	remote, err := d.RemoteConfigDigest(ctx, parsed, local.Architecture)
	if err != nil {
		return refResult{err: fmt.Errorf("remote %s: %w", ref, err)}
	}
	return refResult{update: normDigest(local.ID) != normDigest(remote)}
}

func normDigest(s string) string { return strings.TrimPrefix(s, "sha256:") }
