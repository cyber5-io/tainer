package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_WordPress(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
project:
  name: my-client
  type: wordpress
  domain: my-client.tainer.me
runtime:
  php: "8.4"
  database: mariadb
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if m.Project.Name != "my-client" {
		t.Errorf("Name = %q, want %q", m.Project.Name, "my-client")
	}
	if m.Project.Type != TypeWordPress {
		t.Errorf("Type = %q, want %q", m.Project.Type, TypeWordPress)
	}
	if m.Runtime.PHP != "8.4" {
		t.Errorf("PHP = %q, want %q", m.Runtime.PHP, "8.4")
	}
	if m.Runtime.Database != "mariadb" {
		t.Errorf("Database = %q, want %q", m.Runtime.Database, "mariadb")
	}
}

func TestLoad_NodeJS(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
project:
  name: api-server
  type: nodejs
  domain: api-server.tainer.me
runtime:
  node: "22"
  database: postgres
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if m.Project.Type != TypeNodeJS {
		t.Errorf("Type = %q, want %q", m.Project.Type, TypeNodeJS)
	}
	if m.Runtime.Node != "22" {
		t.Errorf("Node = %q, want %q", m.Runtime.Node, "22")
	}
}

func TestLoad_InvalidVersion(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 2
project:
  name: test
  type: wordpress
  domain: test.tainer.me
runtime:
  php: "8.4"
  database: mariadb
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() should error on version 2")
	}
}

func TestLoad_InvalidType(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
project:
  name: test
  type: django
  domain: test.tainer.me
runtime:
  database: postgres
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() should error on invalid type")
	}
}

func TestLoad_DatabaseNone(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
project:
  name: static
  type: nodejs
  domain: static.tainer.me
runtime:
  node: "22"
  database: none
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if m.Runtime.Database != DatabaseNone {
		t.Errorf("Database = %q, want %q", m.Runtime.Database, DatabaseNone)
	}
}

func TestLoad_InvalidProjectName(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
project:
  name: My-Client
  type: wordpress
  domain: my-client.tainer.me
runtime:
  php: "8.4"
  database: mariadb
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() should error on invalid project name")
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	if Exists(dir) {
		t.Error("Exists() should be false in empty dir")
	}
	os.WriteFile(filepath.Join(dir, "tainer.yaml"), []byte("version: 1"), 0644)
	if !Exists(dir) {
		t.Error("Exists() should be true when tainer.yaml present")
	}
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Version: 1,
		Project: ProjectConfig{Name: "test", Type: TypeWordPress, Domain: "test.tainer.me"},
		Runtime: RuntimeConfig{PHP: "8.4", Database: DatabaseMariaDB},
	}
	path := filepath.Join(dir, "tainer.yaml")
	err := Save(m, path)
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after Save() error: %v", err)
	}
	if loaded.Project.Name != "test" {
		t.Errorf("roundtrip Name = %q, want %q", loaded.Project.Name, "test")
	}
}

func TestValidateMounts_Valid(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
project:
  name: test
  type: wordpress
  domain: test.tainer.me
runtime:
  php: "8.4"
  database: mariadb
mounts:
  - media
  - uploads
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)
	_, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMounts_Slash(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
project:
  name: test
  type: wordpress
  domain: test.tainer.me
runtime:
  php: "8.4"
  database: mariadb
mounts:
  - some/path
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for path with slash")
	}
}

func TestValidateMounts_Reserved(t *testing.T) {
	for _, name := range []string{"app", "data", "db"} {
		dir := t.TempDir()
		yaml := fmt.Sprintf(`version: 1
project:
  name: test
  type: wordpress
  domain: test.tainer.me
runtime:
  php: "8.4"
  database: mariadb
mounts:
  - %s
`, name)
		path := filepath.Join(dir, "tainer.yaml")
		os.WriteFile(path, []byte(yaml), 0644)
		_, err := Load(path)
		if err == nil {
			t.Errorf("expected error for reserved mount name %q", name)
		}
	}
}

func TestValidateMounts_Empty(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
project:
  name: test
  type: wordpress
  domain: test.tainer.me
runtime:
  php: "8.4"
  database: mariadb
mounts:
  - ""
`
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(yaml), 0644)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for empty mount name")
	}
}

func TestContainerMountBase(t *testing.T) {
	php := &Manifest{Project: ProjectConfig{Type: TypeWordPress}}
	if php.ContainerMountBase() != "/var/www" {
		t.Errorf("expected /var/www for PHP, got %s", php.ContainerMountBase())
	}

	node := &Manifest{Project: ProjectConfig{Type: TypeNodeJS}}
	if node.ContainerMountBase() != "/" {
		t.Errorf("expected / for Node.js, got %s", node.ContainerMountBase())
	}
}

func TestContainerAppPath(t *testing.T) {
	wp := &Manifest{Project: ProjectConfig{Type: TypeWordPress}}
	if wp.ContainerAppPath() != "/var/www/html" {
		t.Errorf("expected /var/www/html, got %s", wp.ContainerAppPath())
	}
	node := &Manifest{Project: ProjectConfig{Type: TypeNodeJS}}
	if node.ContainerAppPath() != "/app" {
		t.Errorf("expected /app, got %s", node.ContainerAppPath())
	}
}

func TestHelperMethods(t *testing.T) {
	wp := &Manifest{Project: ProjectConfig{Type: TypeWordPress}, Runtime: RuntimeConfig{PHP: "8.4", Database: DatabaseMariaDB}}
	if !wp.IsPHP() {
		t.Error("WordPress should be PHP")
	}
	if wp.IsNode() {
		t.Error("WordPress should not be Node")
	}
	if wp.RuntimeVersion() != "8.4" {
		t.Errorf("RuntimeVersion() = %q, want %q", wp.RuntimeVersion(), "8.4")
	}
	if !wp.HasDatabase() {
		t.Error("should have database")
	}
	if wp.DBPort() != "3306" {
		t.Errorf("DBPort() = %q, want 3306", wp.DBPort())
	}

	node := &Manifest{Project: ProjectConfig{Type: TypeNodeJS}, Runtime: RuntimeConfig{Node: "22", Database: DatabasePostgres}}
	if node.IsPHP() {
		t.Error("NodeJS should not be PHP")
	}
	if !node.IsNode() {
		t.Error("NodeJS should be Node")
	}
	if node.DBPort() != "5432" {
		t.Errorf("DBPort() = %q, want 5432", node.DBPort())
	}

	none := &Manifest{Project: ProjectConfig{Type: TypeNodeJS}, Runtime: RuntimeConfig{Node: "22", Database: DatabaseNone}}
	if none.HasDatabase() {
		t.Error("should not have database")
	}
}

func TestPHPLimits_Defaults(t *testing.T) {
	l := PHPLimits{}
	r := l.Resolved()
	if r.UploadMaxFilesize != "2G" {
		t.Errorf("UploadMaxFilesize = %q, want 2G", r.UploadMaxFilesize)
	}
	if r.MemoryLimit != "512M" {
		t.Errorf("MemoryLimit = %q, want 512M", r.MemoryLimit)
	}
	if r.MaxInputVars != "10000" {
		t.Errorf("MaxInputVars = %q, want 10000", r.MaxInputVars)
	}
}

func TestPHPLimits_Override(t *testing.T) {
	l := PHPLimits{MemoryLimit: "256M", MaxInputVars: "5000"}
	r := l.Resolved()
	if r.MemoryLimit != "256M" {
		t.Errorf("MemoryLimit = %q, want 256M", r.MemoryLimit)
	}
	if r.MaxInputVars != "5000" {
		t.Errorf("MaxInputVars = %q, want 5000", r.MaxInputVars)
	}
	if r.UploadMaxFilesize != "2G" {
		t.Errorf("UploadMaxFilesize should default to 2G, got %q", r.UploadMaxFilesize)
	}
}

func TestPHPLimits_EnvFlags(t *testing.T) {
	l := PHPLimits{MemoryLimit: "256M"}
	flags := l.EnvFlags()
	if len(flags) != 10 {
		t.Fatalf("expected 10 flags (5 pairs), got %d", len(flags))
	}
	// Check memory_limit override
	for i := 0; i < len(flags)-1; i++ {
		if flags[i] == "-e" && flags[i+1] == "PHP_MEMORY_LIMIT=256M" {
			return
		}
	}
	t.Error("expected PHP_MEMORY_LIMIT=256M in flags")
}

func TestLoad_WithLimits(t *testing.T) {
	dir := t.TempDir()
	content := "version: 1\nproject:\n  name: test\n  type: wordpress\n  domain: test.tainer.me\nruntime:\n  php: \"8.4\"\n  database: mariadb\n  limits:\n    memory_limit: 256M\n    upload_max_filesize: 1G\n"
	path := filepath.Join(dir, "tainer.yaml")
	os.WriteFile(path, []byte(content), 0644)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if m.Runtime.Limits.MemoryLimit != "256M" {
		t.Errorf("Limits.MemoryLimit = %q, want 256M", m.Runtime.Limits.MemoryLimit)
	}
	r := m.Runtime.Limits.Resolved()
	if r.UploadMaxFilesize != "1G" {
		t.Errorf("Resolved UploadMaxFilesize = %q, want 1G", r.UploadMaxFilesize)
	}
	if r.MaxExecutionTime != "300" {
		t.Errorf("Resolved MaxExecutionTime = %q, want 300 (default)", r.MaxExecutionTime)
	}
}
