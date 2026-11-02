package imagecheck

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
)

const maxConcurrency = 6

// Deps are the outside-world lookups Run needs; tests fake all three.
type Deps struct {
	EnginePods         func(ctx context.Context) ([]pod.Pod, error)
	ImageInspect       func(ctx context.Context, ref string) (engine.ImageInfo, error)
	RemoteConfigDigest func(ctx context.Context, ref Ref, arch string) (string, error)
}

// Report is the `tainer update --check --json` document. The envelope's
// "v" key is added by the JSON writer.
type Report struct {
	CheckedAt string      `json:"checkedAt"`
	Pods      []PodReport `json:"pods"`
	UpToDate  int         `json:"upToDate"`
	Available int         `json:"available"`
	Unknown   int         `json:"unknown"`
}

type PodReport struct {
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	Domain          string       `json:"domain"`
	State           string       `json:"state"`
	UpdateAvailable bool         `json:"updateAvailable"`
	Parts           string       `json:"parts"`
	Roles           []RoleReport `json:"roles"`
	Error           string       `json:"error,omitempty"`
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
}

// Run compares each registered project's local images with the registry,
// metadata only. Projects with no engine pod are left out.
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
		if _, ok := byName[n]; ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	plans := make([]podPlan, 0, len(names))
	unique := map[string]struct{}{}
	for _, n := range names {
		pl := podPlan{name: n, state: "running"}
		if byName[n].State() == pod.StateStopped {
			pl.state = "stopped"
		}
		m, err := manifest.Load(filepath.Join(projects[n].Path, manifest.FileName))
		if err != nil {
			pl.err = fmt.Errorf("manifest: %w", err)
			plans = append(plans, pl)
			continue
		}
		pl.m = m
		for _, role := range pod.RolesForPod(m) {
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
		for _, r := range pl.roles {
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
		case pr.Error != "":
			rep.Unknown++
		default:
			rep.UpToDate++
		}
		rep.Pods = append(rep.Pods, pr)
	}
	return rep, nil
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
