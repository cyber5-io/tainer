package project

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
	tainerRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
)

const imageRegistry = "ghcr.io/cyber5-io/tainer"

// RegistryImage returns the registry image reference with a specific tag.
// Example: RegistryImage("nextjs", "15") → "ghcr.io/cyber5-io/tainer-nextjs:15"
func RegistryImage(name, tag string) string {
	return fmt.Sprintf("%s-%s:%s", imageRegistry, name, tag)
}

// MainImage returns the registry image for the project's main container.
func MainImage(m *manifest.Manifest) string {
	name := string(m.Project.Type)
	tag := "latest"

	switch m.Project.Type {
	case manifest.TypeWordPress, manifest.TypePHP:
		tag = m.Runtime.PHP
	case manifest.TypeNodeJS:
		tag = m.Runtime.Node
	case manifest.TypeNextJS:
		tag = frameworkVersion(m.Project.Type)
	case manifest.TypeNuxtJS:
		tag = frameworkVersion(m.Project.Type)
	}

	return RegistryImage(name, tag)
}

// frameworkVersion maps project types to their framework version tag.
// These correspond to the pre-built images on the registry.
func frameworkVersion(pt manifest.ProjectType) string {
	switch pt {
	case manifest.TypeNextJS:
		return "15"
	case manifest.TypeNuxtJS:
		return "3"
	default:
		return "latest"
	}
}

// PullImages pulls all required container images for a project from the registry.
func PullImages(m *manifest.Manifest) error {
	// Main container
	if err := pullImage(MainImage(m)); err != nil {
		return fmt.Errorf("pulling %s image: %w", m.Project.Type, err)
	}

	// PHP projects also need a shared phpfpm container
	if m.IsPHP() {
		if err := pullImage(RegistryImage("phpfpm", m.Runtime.PHP)); err != nil {
			return fmt.Errorf("pulling phpfpm image: %w", err)
		}
	}

	// Database
	if m.HasDatabase() {
		if err := pullImage(RegistryImage(string(m.Runtime.Database), "latest")); err != nil {
			return fmt.Errorf("pulling database image: %w", err)
		}
	}

	return nil
}

// PullImagesVerbose pulls images and shows pull output (for tainer update).
func PullImagesVerbose(m *manifest.Manifest) error {
	if err := pullImageVerbose(MainImage(m)); err != nil {
		return fmt.Errorf("pulling %s image: %w", m.Project.Type, err)
	}
	if m.IsPHP() {
		if err := pullImageVerbose(RegistryImage("phpfpm", m.Runtime.PHP)); err != nil {
			return fmt.Errorf("pulling phpfpm image: %w", err)
		}
	}
	if m.HasDatabase() {
		if err := pullImageVerbose(RegistryImage(string(m.Runtime.Database), "latest")); err != nil {
			return fmt.Errorf("pulling database image: %w", err)
		}
	}
	return nil
}

func pullImage(image string) error {
	// Skip pull if image already exists locally — enables fully offline starts
	if tainerRegistry.ImageExistsLocally(image) {
		return nil
	}
	cmd := exec.Command("tainer", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s is not available locally and registry is unreachable (offline?): %s", image, string(output))
	}
	return nil
}

func pullImageVerbose(image string) error {
	// Strip registry prefix for cleaner output
	short := strings.TrimPrefix(image, imageRegistry+"-")
	fmt.Printf("  %s... ", short)
	cmd := exec.Command("tainer", "pull", image)
	_, err := cmd.CombinedOutput()
	if err != nil {
		if tainerRegistry.ImageExistsLocally(image) {
			fmt.Println("[CACHED]")
			return nil
		}
		fmt.Println("[FAIL]")
		return fmt.Errorf("could not pull %s", short)
	}
	fmt.Println("[OK]")
	return nil
}
