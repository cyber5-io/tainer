package pod

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/network"
	"github.com/cyber5-io/tainer/pkg/tainer/router"
)

// DestroyMode selects the destroy variant.
type DestroyMode int

const (
	DestroyDefault DestroyMode = iota
	DestroyClean
	DestroyNuke
)

// Destroy removes the pod's containers + network. Behaviour by mode:
//   - DestroyDefault: containers + network only; project files untouched
//   - DestroyClean:   plus tainer-managed files in project dir; data/ kept
//   - DestroyNuke:    plus data/; requires interactive name confirmation
func Destroy(ctx context.Context, eng *engine.Client, podName, projectDir string, mode DestroyMode, prompt func() string) error {
	if mode == DestroyNuke {
		if err := confirmNuke(podName, prompt); err != nil {
			return err
		}
	}

	p, err := Get(ctx, eng, podName)
	if err != nil {
		return err
	}
	if p != nil {
		for _, c := range p.Containers {
			if err := eng.Remove(ctx, c.Name, true); err != nil {
				return fmt.Errorf("remove %s: %w", c.Name, err)
			}
		}
	}
	if err := router.DetachFromPod(ctx, eng, podName); err != nil {
		return err
	}
	if err := eng.NetworkRemove(ctx, NetworkName(podName)); err != nil {
		return err
	}
	network.FreeSubnet(podName)

	// Release the registry slot so the pod ID is reusable.
	if err := FreePodID(podName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: free pod id %q: %v\n", podName, err)
	}

	if mode == DestroyClean || mode == DestroyNuke {
		if err := cleanProjectDir(projectDir); err != nil {
			return err
		}
	}
	if mode == DestroyNuke {
		if err := os.RemoveAll(filepath.Join(projectDir, "data")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("rm data/: %w", err)
		}
	}
	return nil
}

// cleanProjectDir removes tainer-managed files from the project dir
// while preserving everything else (user code, data/, etc).
func cleanProjectDir(dir string) error {
	allowlist := []string{
		".tainer-authorized_keys",
		".tainer.local.staging",
	}
	for _, name := range allowlist {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// confirmNuke prompts the user to type the pod name verbatim.
func confirmNuke(podName string, prompt func() string) error {
	fmt.Fprintf(os.Stderr, "Type %q to confirm nuking ALL data: ", podName)
	got := strings.TrimSpace(prompt())
	if got != podName {
		return fmt.Errorf("confirmation %q did not match pod name %q", got, podName)
	}
	return nil
}
