package manifest

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func init() {
	applyOverlayFunc = applyOverlayImpl
}

var applyOverlayFunc func(m *Manifest, path string) error

// applyOverlayImpl reads the overlay YAML, decodes it into the same
// Manifest struct, and copies non-zero fields onto m. Deep-merge
// semantics: maps and slices on the overlay fully replace the base
// (no per-key merge inside Ports, Containers, Mounts) — matches DDEV's
// config.local.yaml behaviour and is easy to reason about.
func applyOverlayImpl(m *Manifest, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var ov Manifest
	if err := yaml.Unmarshal(data, &ov); err != nil {
		return fmt.Errorf("overlay parse: %w", err)
	}
	mergeIntoBase(m, &ov)
	return nil
}

func mergeIntoBase(base, ov *Manifest) {
	if ov.Version != 0 {
		base.Version = ov.Version
	}
	mergeProject(&base.Project, &ov.Project)
	mergeRuntime(&base.Runtime, &ov.Runtime)
	mergePod(base.Pod, ov.Pod)
	if len(ov.Mounts) > 0 {
		base.Mounts = ov.Mounts
	}
	if len(ov.Ports) > 0 {
		base.Ports = ov.Ports
	}
}

func mergeProject(b, ov *ProjectConfig) {
	if ov.Name != "" {
		b.Name = ov.Name
	}
	if ov.Type != "" {
		b.Type = ov.Type
	}
	if ov.Domain != "" {
		b.Domain = ov.Domain
	}
	if ov.AutoOpen != nil {
		b.AutoOpen = ov.AutoOpen
	}
}

func mergeRuntime(b, ov *RuntimeConfig) {
	if ov.PHP != "" {
		b.PHP = ov.PHP
	}
	if ov.Node != "" {
		b.Node = ov.Node
	}
	if ov.Database != "" {
		b.Database = ov.Database
	}
	if ov.Shell != "" {
		b.Shell = ov.Shell
	}
	if ov.BuildDir != "" {
		b.BuildDir = ov.BuildDir
	}
	mergePHPLimits(&b.PHPLimits, &ov.PHPLimits)
}

func mergePHPLimits(b, ov *PHPLimits) {
	if ov.UploadMaxFilesize != "" {
		b.UploadMaxFilesize = ov.UploadMaxFilesize
	}
	if ov.PostMaxSize != "" {
		b.PostMaxSize = ov.PostMaxSize
	}
	if ov.MemoryLimit != "" {
		b.MemoryLimit = ov.MemoryLimit
	}
	if ov.MaxExecutionTime != "" {
		b.MaxExecutionTime = ov.MaxExecutionTime
	}
	if ov.MaxInputVars != "" {
		b.MaxInputVars = ov.MaxInputVars
	}
}

func mergePod(b, ov *PodConfig) {
	if ov == nil {
		return
	}
	// Assume b is non-nil (defaults() ensures this)
	if ov.Size != "" {
		b.Size = ov.Size
	}
	if len(ov.Containers) > 0 {
		if b.Containers == nil {
			b.Containers = make(map[string]ContainerLimits)
		}
		for k, v := range ov.Containers {
			b.Containers[k] = v
		}
	}
}
