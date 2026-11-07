package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/filelock"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

type Project struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	Domain  string `json:"domain"`
	Created string `json:"created"`
	// ImageVersion is the sealed control image version last provisioned
	// (control pod only).
	ImageVersion string `json:"imageVersion,omitempty"`
}

type registryData struct {
	Projects map[string]Project `json:"projects"`
}

// mu serialises this process's goroutines; the file lock next to
// projects.json serialises tainer processes (a command and the detached
// control child routinely run side by side). Every call reads the file
// afresh, so no process acts on another's stale copy.
var (
	mu         sync.Mutex
	warnedOnce bool
)

func lockPath() string { return config.ProjectsFile() + ".lock" }

// load reads projects.json. A missing file is an empty registry.
func load() *registryData {
	r := &registryData{Projects: make(map[string]Project)}
	data, err := os.ReadFile(config.ProjectsFile())
	if err != nil {
		return r
	}
	if err := json.Unmarshal(data, r); err != nil {
		if !warnedOnce {
			warnedOnce = true
			fmt.Fprintf(os.Stderr, "Warning: corrupted projects.json, starting fresh: %v\n", err)
		}
		r.Projects = make(map[string]Project)
	}
	if r.Projects == nil {
		r.Projects = make(map[string]Project)
	}
	return r
}

// update runs fn on a fresh read of the registry with both locks held and
// saves the result when fn reports a change.
func update(fn func(r *registryData) (changed bool, err error)) error {
	mu.Lock()
	defer mu.Unlock()
	if err := config.EnsureDirs(); err != nil {
		return fmt.Errorf("creating config dirs: %w", err)
	}
	release, err := filelock.Lock(lockPath())
	if err != nil {
		return fmt.Errorf("lock the project list: %w", err)
	}
	defer release()
	r := load()
	changed, err := fn(r)
	if err != nil || !changed {
		return err
	}
	return save(r)
}

// read runs fn on a fresh read of the registry.
func read(fn func(r *registryData)) {
	mu.Lock()
	defer mu.Unlock()
	fn(load())
}

// save writes r atomically: readers never see a half-written file.
func save(r *registryData) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	path := config.ProjectsFile()
	tmp := path + ".partial"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func Add(name, path, projType, domain string) error {
	return update(func(r *registryData) (bool, error) {
		if existing, ok := r.Projects[name]; ok && existing.Path != path {
			// A different live project owns this name — refuse. But if the old
			// path is gone (deleted dir, tmpfs from a test run, etc), silently
			// reclaim the slot. Stale entries shouldn't block legitimate inits.
			if manifest.Exists(existing.Path) {
				return false, fmt.Errorf("project name %q is already registered at %s", name, existing.Path)
			}
		}
		r.Projects[name] = Project{
			Path:    path,
			Type:    projType,
			Domain:  domain,
			Created: time.Now().UTC().Format(time.RFC3339),
		}
		return true, nil
	})
}

func Get(name string) (p Project, ok bool) {
	read(func(r *registryData) { p, ok = r.Projects[name] })
	return p, ok
}

func Remove(name string) {
	_ = update(func(r *registryData) (bool, error) {
		delete(r.Projects, name)
		return true, nil
	})
}

func All() map[string]Project {
	var result map[string]Project
	read(func(r *registryData) { result = r.Projects })
	return result
}

// SelfHeal validates all registered project paths.
// Removes entries whose path no longer exists or no longer contains tainer.yaml.
// Returns the names of pruned projects.
func SelfHeal() []string {
	var pruned []string
	_ = update(func(r *registryData) (bool, error) {
		for name, p := range r.Projects {
			if !manifest.Exists(p.Path) {
				delete(r.Projects, name)
				pruned = append(pruned, name)
			}
		}
		return len(pruned) > 0, nil
	})
	return pruned
}

// FindByPath returns the project name registered for a given directory path.
func FindByPath(path string) (name string, found bool) {
	read(func(r *registryData) {
		for n, p := range r.Projects {
			if p.Path == path {
				name, found = n, true
				return
			}
		}
	})
	return name, found
}

// Put stores p under name as given, replacing any existing entry. Unlike Add
// it neither refuses a live conflicting path nor resets Created/ImageVersion —
// it is for tainer's own bookkeeping (re-pointing the pinned control project).
func Put(name string, p Project) error {
	return update(func(r *registryData) (bool, error) {
		r.Projects[name] = p
		return true, nil
	})
}
