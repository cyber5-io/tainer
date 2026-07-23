package pod

import (
	"os"
	"path/filepath"
	"testing"
)

// An old project (pre-secrets-store) whose DB was already initialized carries
// its DB credentials in the project .env. Adopting them — instead of minting
// fresh random passwords that no live DB would accept — is what lets such a
// project start on the new tainer without an auth mismatch.
func TestLoadOrCreateSecrets_SeedsFromProjectEnv(t *testing.T) {
	dir := t.TempDir()               // project dir
	secretsPath := filepath.Join(t.TempDir(), "mywp.env")
	envBody := "DB_NAME=tainer\nDB_USER=tainer\n" +
		"DB_PASSWORD=the-existing-db-password-32chars0\n" +
		"DB_ROOT_PASSWORD=the-existing-root-password-32chr\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envBody), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadOrCreateSecretsAt(secretsPath, "mywp", dir)
	if err != nil {
		t.Fatalf("LoadOrCreateSecretsAt: %v", err)
	}
	if s.DBPassword != "the-existing-db-password-32chars0" {
		t.Errorf("DBPassword = %q, want the .env value (adopted)", s.DBPassword)
	}
	if s.DBRootPassword != "the-existing-root-password-32chr" {
		t.Errorf("DBRootPassword = %q, want the .env value (adopted)", s.DBRootPassword)
	}
	// Persisted secret must equal the adopted creds, so later loads are stable.
	reloaded, err := LoadOrCreateSecretsAt(secretsPath, "mywp", dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.DBPassword != s.DBPassword {
		t.Errorf("persisted secret drifted: %q != %q", reloaded.DBPassword, s.DBPassword)
	}
}

func TestLoadOrCreateSecrets_RandomWhenNoEnvCreds(t *testing.T) {
	dir := t.TempDir() // project dir with no .env
	secretsPath := filepath.Join(t.TempDir(), "fresh.env")

	s, err := LoadOrCreateSecretsAt(secretsPath, "fresh", dir)
	if err != nil {
		t.Fatalf("LoadOrCreateSecretsAt: %v", err)
	}
	if s.DBPassword == "" || s.DBRootPassword == "" {
		t.Error("expected generated non-empty passwords for a project with no .env")
	}
	if s.DBName != "tainer" || s.DBUser != "tainer" {
		t.Errorf("defaults wrong: name=%q user=%q", s.DBName, s.DBUser)
	}
}

func TestLoadOrCreateSecrets_ExistingSecretIgnoresEnv(t *testing.T) {
	dir := t.TempDir()
	secretsPath := filepath.Join(t.TempDir(), "mywp.env")
	// Pre-existing secret.
	if err := os.WriteFile(secretsPath,
		[]byte("DB_NAME=tainer\nDB_USER=tainer\nDB_PASSWORD=already-here\nDB_ROOT_PASSWORD=already-root\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A .env that must NOT override the existing secret.
	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("DB_PASSWORD=env-value\nDB_ROOT_PASSWORD=env-root\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadOrCreateSecretsAt(secretsPath, "mywp", dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.DBPassword != "already-here" {
		t.Errorf("DBPassword = %q, want the pre-existing secret (env must not override)", s.DBPassword)
	}
}
