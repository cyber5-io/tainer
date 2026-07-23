package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

const v1Fixture = `version: 1
project:
  name: my-client
  type: wordpress
  domain: my-client.tainer.me
runtime:
  php: "8.4"
  database: mariadb
`

func TestPersistV2Migration_UpgradesV1AndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tainer.yaml")
	if err := os.WriteFile(path, []byte(v1Fixture), 0644); err != nil {
		t.Fatal(err)
	}

	migrated, err := PersistV2Migration(path)
	if err != nil {
		t.Fatalf("PersistV2Migration: %v", err)
	}
	if !migrated {
		t.Fatal("expected migrated=true for a v1 file")
	}

	// Original preserved verbatim in the backup.
	bak, err := os.ReadFile(path + ".v1.bak")
	if err != nil {
		t.Fatalf("backup not written: %v", err)
	}
	if string(bak) != v1Fixture {
		t.Errorf("backup does not match original:\n%s", bak)
	}

	// The live file is now v2 and reloads without needing migration.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.Version != 2 {
		t.Errorf("on-disk version = %d, want 2 after persist", probe.Version)
	}

	// Reloaded manifest keeps the project identity.
	m, err := Load(path)
	if err != nil {
		t.Fatalf("reload after persist: %v", err)
	}
	if m.Project.Name != "my-client" || m.Project.Type != TypeWordPress {
		t.Errorf("identity lost after persist: %+v", m.Project)
	}
}

func TestPersistV2Migration_AlreadyV2IsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tainer.yaml")
	// First migrate a v1 to v2 on disk.
	if err := os.WriteFile(path, []byte(v1Fixture), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PersistV2Migration(path); err != nil {
		t.Fatal(err)
	}
	v2 := mustRead(t, path)

	// A second call must be a no-op: no re-migration, no second backup churn.
	migrated, err := PersistV2Migration(path)
	if err != nil {
		t.Fatalf("second PersistV2Migration: %v", err)
	}
	if migrated {
		t.Error("expected migrated=false for an already-v2 file")
	}
	if got := mustRead(t, path); got != v2 {
		t.Errorf("already-v2 file was rewritten:\nbefore=%q\nafter=%q", v2, got)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
