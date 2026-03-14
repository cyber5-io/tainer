package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/containers/podman/v6/pkg/tainer/validate"
	"gopkg.in/yaml.v3"
)

const FileName = "tainer.yaml"

type ProjectType string

const (
	TypeWordPress ProjectType = "wordpress"
	TypePHP       ProjectType = "php"
	TypeNodeJS    ProjectType = "nodejs"
	TypeKompozi   ProjectType = "kompozi"
)

type DatabaseType string

const (
	DatabaseMariaDB  DatabaseType = "mariadb"
	DatabasePostgres DatabaseType = "postgres"
	DatabaseNone     DatabaseType = "none"
)

type Manifest struct {
	Version int           `yaml:"version"`
	Project ProjectConfig `yaml:"project"`
	Runtime RuntimeConfig `yaml:"runtime"`
}

type ProjectConfig struct {
	Name   string      `yaml:"name"`
	Type   ProjectType `yaml:"type"`
	Domain string      `yaml:"domain"`
}

type RuntimeConfig struct {
	PHP      string       `yaml:"php,omitempty"`
	Node     string       `yaml:"node,omitempty"`
	Database DatabaseType `yaml:"database"`
}

func (m *Manifest) IsPHP() bool {
	return m.Project.Type == TypeWordPress || m.Project.Type == TypePHP
}

func (m *Manifest) IsNode() bool {
	return m.Project.Type == TypeNodeJS || m.Project.Type == TypeKompozi
}

func (m *Manifest) RuntimeVersion() string {
	if m.IsPHP() {
		return m.Runtime.PHP
	}
	return m.Runtime.Node
}

func (m *Manifest) HasDatabase() bool {
	return m.Runtime.Database != DatabaseNone
}

func (m *Manifest) DBPort() string {
	if m.Runtime.Database == DatabasePostgres {
		return "5432"
	}
	return "3306"
}

func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil
}

func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func LoadFromDir(dir string) (*Manifest, error) {
	return Load(filepath.Join(dir, FileName))
}

func Save(m *Manifest, path string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (m *Manifest) validate() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported manifest version: %d (expected 1)", m.Version)
	}
	if err := validate.ProjectName(m.Project.Name); err != nil {
		return fmt.Errorf("invalid project name: %w", err)
	}
	switch m.Project.Type {
	case TypeWordPress, TypePHP, TypeNodeJS, TypeKompozi:
	default:
		return fmt.Errorf("invalid project type: %q (expected wordpress, php, nodejs, or kompozi)", m.Project.Type)
	}
	switch m.Runtime.Database {
	case DatabaseMariaDB, DatabasePostgres, DatabaseNone:
	default:
		return fmt.Errorf("invalid database: %q (expected mariadb, postgres, or none)", m.Runtime.Database)
	}
	if m.IsPHP() && m.Runtime.PHP == "" {
		return fmt.Errorf("php version required for %s projects", m.Project.Type)
	}
	if m.IsNode() && m.Runtime.Node == "" {
		return fmt.Errorf("node version required for %s projects", m.Project.Type)
	}
	return nil
}
