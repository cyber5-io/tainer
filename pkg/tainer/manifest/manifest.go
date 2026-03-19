package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/validate"
	"gopkg.in/yaml.v3"
)

const FileName = "tainer.yaml"

type ProjectType string

const (
	TypeWordPress ProjectType = "wordpress"
	TypePHP       ProjectType = "php"
	TypeNodeJS    ProjectType = "nodejs"
	TypeNextJS    ProjectType = "nextjs"
	TypeNuxtJS    ProjectType = "nuxtjs"
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
	Mounts  []string      `yaml:"mounts,omitempty"`
}

type ProjectConfig struct {
	Name     string      `yaml:"name"`
	Type     ProjectType `yaml:"type"`
	Domain   string      `yaml:"domain"`
	AutoOpen *bool       `yaml:"auto-open,omitempty"`
}

type PHPLimits struct {
	UploadMaxFilesize string `yaml:"upload_max_filesize,omitempty"`
	PostMaxSize       string `yaml:"post_max_size,omitempty"`
	MemoryLimit       string `yaml:"memory_limit,omitempty"`
	MaxExecutionTime  string `yaml:"max_execution_time,omitempty"`
	MaxInputVars      string `yaml:"max_input_vars,omitempty"`
}

var DefaultPHPLimits = PHPLimits{
	UploadMaxFilesize: "2G",
	PostMaxSize:       "2G",
	MemoryLimit:       "512M",
	MaxExecutionTime:  "300",
	MaxInputVars:      "10000",
}

func (l PHPLimits) Resolved() PHPLimits {
	d := DefaultPHPLimits
	if l.UploadMaxFilesize != "" {
		d.UploadMaxFilesize = l.UploadMaxFilesize
	}
	if l.PostMaxSize != "" {
		d.PostMaxSize = l.PostMaxSize
	}
	if l.MemoryLimit != "" {
		d.MemoryLimit = l.MemoryLimit
	}
	if l.MaxExecutionTime != "" {
		d.MaxExecutionTime = l.MaxExecutionTime
	}
	if l.MaxInputVars != "" {
		d.MaxInputVars = l.MaxInputVars
	}
	return d
}

func (l PHPLimits) EnvFlags() []string {
	r := l.Resolved()
	return []string{
		"-e", "PHP_UPLOAD_MAX_FILESIZE=" + r.UploadMaxFilesize,
		"-e", "PHP_POST_MAX_SIZE=" + r.PostMaxSize,
		"-e", "PHP_MEMORY_LIMIT=" + r.MemoryLimit,
		"-e", "PHP_MAX_EXECUTION_TIME=" + r.MaxExecutionTime,
		"-e", "PHP_MAX_INPUT_VARS=" + r.MaxInputVars,
	}
}

type RuntimeConfig struct {
	PHP      string       `yaml:"php,omitempty"`
	Node     string       `yaml:"node,omitempty"`
	Database DatabaseType `yaml:"database"`
	Limits   PHPLimits    `yaml:"limits,omitempty"`
	Shell    string       `yaml:"shell,omitempty"`
}

func (m *Manifest) ShellOrDefault() string {
	if m.Runtime.Shell != "" {
		return m.Runtime.Shell
	}
	return "zsh"
}

func (m *Manifest) IsPHP() bool {
	return m.Project.Type == TypeWordPress || m.Project.Type == TypePHP
}

func (m *Manifest) IsNode() bool {
	return m.Project.Type == TypeNodeJS || m.Project.Type == TypeNextJS ||
		m.Project.Type == TypeNuxtJS || m.Project.Type == TypeKompozi
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

// ContainerMountBase returns the base path for top-level mounts in the container.
func (m *Manifest) ContainerMountBase() string {
	return "/var/www"
}

// ContainerAppPath returns the container path where source code is mounted.
func (m *Manifest) ContainerAppPath() string {
	return "/var/www/html"
}

// HostAppDir returns the name of the source code directory on the host.
func (m *Manifest) HostAppDir() string {
	return "html"
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
	case TypeWordPress, TypePHP, TypeNodeJS, TypeNextJS, TypeNuxtJS, TypeKompozi:
	default:
		return fmt.Errorf("invalid project type: %q (expected wordpress, php, nodejs, nextjs, nuxtjs, or kompozi)", m.Project.Type)
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
	reserved := map[string]bool{"html": true, "data": true, "db": true}
	for _, mount := range m.Mounts {
		if mount == "" {
			return fmt.Errorf("mount name cannot be empty")
		}
		if strings.Contains(mount, "/") || strings.Contains(mount, "..") {
			return fmt.Errorf("mount name must be a simple directory name: %q", mount)
		}
		if reserved[mount] {
			return fmt.Errorf("mount name %q is reserved", mount)
		}
	}
	return nil
}
