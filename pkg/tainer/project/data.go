package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

// DataAdd adds a persistent mount path to the project.
func DataAdd(projectDir, mountPath string) error {
	if err := validateMountPath(mountPath); err != nil {
		return err
	}

	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}

	// Check for duplicates (defaults + user mounts)
	for _, existing := range m.AllDataMounts() {
		if existing == mountPath {
			return fmt.Errorf("mount path %q already exists", mountPath)
		}
	}

	// Create data directory
	dataPath := filepath.Join(projectDir, "data", mountPath)
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	// Update manifest
	m.Data.Mounts = append(m.Data.Mounts, mountPath)
	if err := manifest.Save(m, filepath.Join(projectDir, manifest.FileName)); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}

	fmt.Printf("Added data mount: %s. Restart project to apply.\n", mountPath)
	return nil
}

// DataDel removes a persistent mount path from the project.
func DataDel(projectDir, mountPath string) error {
	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}

	// Cannot remove default mounts
	for _, def := range m.DefaultDataMounts() {
		if def == mountPath {
			return fmt.Errorf("cannot remove default mount %q — only user-added mounts can be removed", mountPath)
		}
	}

	// Find and remove from user mounts
	found := false
	var updated []string
	for _, existing := range m.Data.Mounts {
		if existing == mountPath {
			found = true
			continue
		}
		updated = append(updated, existing)
	}
	if !found {
		return fmt.Errorf("mount path %q not found in data.mounts", mountPath)
	}

	m.Data.Mounts = updated
	if err := manifest.Save(m, filepath.Join(projectDir, manifest.FileName)); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}

	fmt.Printf("Removed data mount: %s. Restart project to apply.\nData files preserved at data/%s/\n", mountPath, mountPath)
	return nil
}

func validateMountPath(path string) error {
	if path == "" {
		return fmt.Errorf("mount path cannot be empty")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("mount path must be relative: %q", path)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return fmt.Errorf("mount path must not contain '..' segments: %q", path)
		}
	}
	return nil
}
