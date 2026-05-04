package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

// ExtractScaffoldToHost extracts a project's scaffold tarball from its image
// directly to the host filesystem, bypassing the slow virtio-fs path that
// would be used if extraction ran inside the container.
//
// Flow:
//  1. Create a scratch container from the image (not started)
//  2. Copy /opt/tainer/scaffold.tar from the container to a host temp file
//  3. Remove the scratch container
//  4. Extract the tarball to the project's app directory on the host
//
// This is 5-10x faster than in-container extraction for scaffolds with large
// node_modules (~150k files): a single bulk file copy + native-fs extract
// instead of 150k virtio-fs file writes.
func ExtractScaffoldToHost(m *manifest.Manifest, projectDir string) error {
	appDir := filepath.Join(projectDir, m.HostAppDir())

	// Skip if the app dir already has content (don't clobber user files)
	if entries, err := os.ReadDir(appDir); err == nil && len(entries) > 0 {
		return nil
	}

	image := MainImage(m)
	scratchName := fmt.Sprintf("tainer-scaffold-%s", m.Project.Name)

	// Ensure scratch container is gone if it was left behind
	exec.Command("tainer", "rm", "-f", scratchName).CombinedOutput() //nolint:errcheck

	// Create a scratch container (not started) just to access image files
	createCmd := exec.Command("tainer", "create", "--name", scratchName, image)
	if _, err := createCmd.CombinedOutput(); err != nil {
		// No scaffold is fine for project types that don't have one
		return nil //nolint:nilerr
	}
	defer exec.Command("tainer", "rm", "-f", scratchName).CombinedOutput() //nolint:errcheck

	// Copy the scaffold tarball to a host temp file
	tmpTar, err := os.CreateTemp("", "tainer-scaffold-*.tar")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpTar.Close()
	defer os.Remove(tmpTar.Name())

	cpCmd := exec.Command("tainer", "cp",
		scratchName+":/opt/tainer/scaffold.tar",
		tmpTar.Name(),
	)
	if output, err := cpCmd.CombinedOutput(); err != nil {
		// Image doesn't have a scaffold — not an error, just skip
		if len(output) > 0 {
			fmt.Fprintf(os.Stderr, "Info: no scaffold in %s: %s\n", image, string(output))
		}
		return nil
	}

	// Ensure the destination exists
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("creating app directory: %w", err)
	}

	// Extract on the host (native APFS / ext4, fast)
	tarCmd := exec.Command("tar", "xf", tmpTar.Name(), "-C", appDir)
	if output, err := tarCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extracting scaffold: %s", string(output))
	}

	return nil
}
