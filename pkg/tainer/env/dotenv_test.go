package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

func TestGenerate_WordPress(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "blog", Type: manifest.TypeWordPress, Domain: "blog.tainer.me"},
		Runtime: manifest.RuntimeConfig{PHP: "8.4", Database: manifest.DatabaseMariaDB},
	}
	path := filepath.Join(dir, ".env")
	err := Generate(m, path)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	for _, key := range []string{"DB_HOST=127.0.0.1", "DB_PORT=3306", "DB_NAME=tainer", "DB_USER=tainer", "MYSQL_DATABASE=tainer", "MYSQL_USER=tainer", "WP_DEBUG=true", "WP_HOME=https://blog.tainer.me"} {
		if !strings.Contains(content, key) {
			t.Errorf(".env missing %q", key)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "DB_PASSWORD=") {
			pw := strings.TrimPrefix(line, "DB_PASSWORD=")
			if len(pw) != 32 {
				t.Errorf("DB_PASSWORD length = %d, want 32", len(pw))
			}
		}
	}
}

func TestGenerate_NodeJS(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "api", Type: manifest.TypeNodeJS, Domain: "api.tainer.me"},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabasePostgres},
	}
	path := filepath.Join(dir, ".env")
	Generate(m, path)
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "DB_PORT=5432") {
		t.Error(".env should have DB_PORT=5432 for postgres")
	}
	if !strings.Contains(content, "POSTGRES_DB=tainer") {
		t.Error(".env should have POSTGRES_DB for postgres")
	}
	if !strings.Contains(content, "DATABASE_URL=") {
		t.Error(".env should have DATABASE_URL for nodejs")
	}
	if strings.Contains(content, "WP_") {
		t.Error(".env should not have WP_ vars for nodejs")
	}
}

func TestGenerate_NoDatabase(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "static", Type: manifest.TypeNodeJS, Domain: "static.tainer.me"},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabaseNone},
	}
	path := filepath.Join(dir, ".env")
	Generate(m, path)
	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, "DB_HOST") {
		t.Error(".env should not have DB vars when database=none")
	}
}

func TestGenerate_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("EXISTING=true"), 0644)
	m := &manifest.Manifest{
		Version: 1,
		Project: manifest.ProjectConfig{Name: "test", Type: manifest.TypePHP, Domain: "test.tainer.me"},
		Runtime: manifest.RuntimeConfig{PHP: "8.4", Database: manifest.DatabaseMariaDB},
	}
	err := Generate(m, path)
	if err != nil {
		t.Fatalf("Generate() should not error on existing file, got: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "EXISTING=true" {
		t.Error("Generate() should not overwrite existing .env")
	}
}
