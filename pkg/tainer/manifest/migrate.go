package manifest

import (
	"gopkg.in/yaml.v3"
)

// v1Manifest is the legacy 0.2.x shape — no pod/ports sections, "limits"
// instead of "php-limits". Migration upgrades the in-memory struct;
// the on-disk file is left untouched until the user explicitly runs
// `tainer migrate` (post-0.9).
type v1Manifest struct {
	Version int             `yaml:"version"`
	Project ProjectConfig   `yaml:"project"`
	Runtime v1RuntimeConfig `yaml:"runtime"`
	Mounts  []string        `yaml:"mounts,omitempty"`
}

type v1RuntimeConfig struct {
	PHP      string       `yaml:"php,omitempty"`
	Node     string       `yaml:"node,omitempty"`
	Database DatabaseType `yaml:"database"`
	Limits   PHPLimits    `yaml:"limits,omitempty"`
	Shell    string       `yaml:"shell,omitempty"`
	BuildDir string       `yaml:"build-dir,omitempty"`
}

func init() {
	migrateV1Func = migrateV1Impl
}

// migrateV1Func is a hook the parser uses to invoke migration without
// importing this file's logic into manifest.go's stub.
var migrateV1Func func(m *Manifest, raw []byte) error

func migrateV1Impl(m *Manifest, raw []byte) error {
	var v1 v1Manifest
	if err := yaml.Unmarshal(raw, &v1); err != nil {
		return err
	}
	m.Version = 2
	m.Project = v1.Project
	m.Runtime = RuntimeConfig{
		PHP:       v1.Runtime.PHP,
		Node:      v1.Runtime.Node,
		Database:  v1.Runtime.Database,
		PHPLimits: v1.Runtime.Limits,
		Shell:     v1.Runtime.Shell,
		BuildDir:  v1.Runtime.BuildDir,
	}
	m.Mounts = v1.Mounts
	// Default pod size for migrated manifests: small.
	m.Pod = &PodConfig{Size: PodSizeSmall}
	// v1 had no ports section. Leave m.Ports nil.
	return nil
}
