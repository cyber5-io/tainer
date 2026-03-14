package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

type Project struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	Domain  string `json:"domain"`
	Created string `json:"created"`
}

type registryData struct {
	Projects map[string]Project `json:"projects"`
}

var (
	registry *registryData
	mu       sync.Mutex
)

func load() *registryData {
	if registry != nil {
		return registry
	}
	registry = &registryData{Projects: make(map[string]Project)}
	data, err := os.ReadFile(config.ProjectsFile())
	if err != nil {
		return registry
	}
	if err := json.Unmarshal(data, registry); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: corrupted projects.json, starting fresh: %v\n", err)
		registry.Projects = make(map[string]Project)
	}
	if registry.Projects == nil {
		registry.Projects = make(map[string]Project)
	}
	return registry
}

func save() error {
	r := load()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(config.ProjectsFile(), data, 0644)
}

func Add(name, path, projType, domain string) error {
	mu.Lock()
	defer mu.Unlock()
	r := load()
	if existing, ok := r.Projects[name]; ok && existing.Path != path {
		return fmt.Errorf("project name %q is already registered at %s", name, existing.Path)
	}
	r.Projects[name] = Project{
		Path:    path,
		Type:    projType,
		Domain:  domain,
		Created: time.Now().UTC().Format(time.RFC3339),
	}
	return save()
}

func Get(name string) (Project, bool) {
	mu.Lock()
	defer mu.Unlock()
	r := load()
	p, ok := r.Projects[name]
	return p, ok
}

func Remove(name string) {
	mu.Lock()
	defer mu.Unlock()
	r := load()
	delete(r.Projects, name)
	save()
}

func All() map[string]Project {
	mu.Lock()
	defer mu.Unlock()
	r := load()
	result := make(map[string]Project, len(r.Projects))
	for k, v := range r.Projects {
		result[k] = v
	}
	return result
}

// SelfHeal validates all registered project paths.
// Removes entries whose path no longer exists or no longer contains tainer.yaml.
// Returns the names of pruned projects.
func SelfHeal() []string {
	mu.Lock()
	defer mu.Unlock()
	r := load()
	var pruned []string
	for name, p := range r.Projects {
		if !manifest.Exists(p.Path) {
			delete(r.Projects, name)
			pruned = append(pruned, name)
		}
	}
	if len(pruned) > 0 {
		save()
	}
	return pruned
}

// FindByPath returns the project name registered for a given directory path.
func FindByPath(path string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	r := load()
	for name, p := range r.Projects {
		if p.Path == path {
			return name, true
		}
	}
	return "", false
}
