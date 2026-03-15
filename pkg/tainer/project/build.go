package project

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

// BuildImages builds all container images for a project from templates.
func BuildImages(m *manifest.Manifest) error {
	tmplDir := filepath.Join(config.TemplatesDir(), string(m.Project.Type))
	prefix := fmt.Sprintf("tainer-%s", m.Project.Name)

	// Build main container (Caddy+SSHD for PHP, Node for Node.js)
	if m.IsPHP() {
		// Alpine PHP packages use version without dots: 8.4 → 84
		alpinePHP := strings.ReplaceAll(m.Runtime.PHP, ".", "")
		if err := buildImage(
			filepath.Join(tmplDir, "Containerfile.caddy"),
			prefix+"-caddy",
			map[string]string{"PHP_VERSION": alpinePHP},
		); err != nil {
			return fmt.Errorf("building caddy image: %w", err)
		}
		if err := buildImage(
			filepath.Join(tmplDir, "Containerfile.phpfpm"),
			prefix+"-phpfpm",
			map[string]string{"PHP_VERSION": m.Runtime.PHP},
		); err != nil {
			return fmt.Errorf("building phpfpm image: %w", err)
		}
	} else {
		if err := buildImage(
			filepath.Join(tmplDir, "Containerfile.node"),
			prefix+"-node",
			map[string]string{"NODE_VERSION": m.Runtime.Node},
		); err != nil {
			return fmt.Errorf("building node image: %w", err)
		}
	}

	// Build database image
	if m.HasDatabase() {
		dbFile := fmt.Sprintf("Containerfile.%s", m.Runtime.Database)
		if err := buildImage(
			filepath.Join(tmplDir, dbFile),
			prefix+"-db",
			nil,
		); err != nil {
			return fmt.Errorf("building database image: %w", err)
		}
	}

	return nil
}

func buildImage(containerfile, tag string, buildArgs map[string]string) error {
	args := []string{"build", "-f", containerfile, "-t", tag}
	for k, v := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}
	// Build context must be templates/ root so COPY shared/... paths resolve
	args = append(args, config.TemplatesDir())

	cmd := exec.Command("tainer", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", tag, string(output))
	}
	return nil
}
