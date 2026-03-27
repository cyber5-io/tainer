package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

// MountAdd adds a custom top-level mount to the project.
func MountAdd(projectDir, name string) error {
	if err := validateMountName(name); err != nil {
		return err
	}

	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}

	for _, existing := range m.Mounts {
		if existing == name {
			return fmt.Errorf("mount %q already exists", name)
		}
	}

	mountPath := filepath.Join(projectDir, name)
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return fmt.Errorf("creating %s directory: %w", name, err)
	}

	m.Mounts = append(m.Mounts, name)
	if err := manifest.Save(m, filepath.Join(projectDir, manifest.FileName)); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}

	return nil
}

// MountDel removes a custom mount from the project.
func MountDel(projectDir, name string) error {
	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}

	found := false
	var updated []string
	for _, existing := range m.Mounts {
		if existing == name {
			found = true
			continue
		}
		updated = append(updated, existing)
	}
	if !found {
		return fmt.Errorf("mount %q not found", name)
	}

	m.Mounts = updated
	if err := manifest.Save(m, filepath.Join(projectDir, manifest.FileName)); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}

	return nil
}

func validateMountName(name string) error {
	if name == "" {
		return fmt.Errorf("mount name cannot be empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("mount name must be a simple directory name: %q", name)
	}
	reserved := map[string]bool{"html": true, "data": true, "db": true}
	if reserved[name] {
		return fmt.Errorf("mount name %q is reserved", name)
	}
	return nil
}
