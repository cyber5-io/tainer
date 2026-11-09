package pod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// An old project (pre-secrets-store) whose DB was already initialized carries
// its DB credentials in the project .env. Adopting them — instead of minting
// fresh random passwords that no live DB would accept — is what lets such a
// project start on the new tainer without an auth mismatch.
func TestLoadOrCreateSecrets_SeedsFromProjectEnv(t *testing.T) {
	dir := t.TempDir() // project dir
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

// Clone-then-start: a fresh clone has no .env (gitignored), so start must
// regenerate it FROM the pod's effective secrets — the database is already
// (or will be) initialized with those values. Node-family apps read .env
// from their own dir (app/), so they get a copy there too; PHP/WordPress
// must NOT (app/ is the web-served docroot).
func TestEnsureProjectEnv(t *testing.T) {
	newManifest := func(typ manifest.ProjectType, db manifest.DatabaseType) *manifest.Manifest {
		m := &manifest.Manifest{
			Version: 2,
			Project: manifest.ProjectConfig{Name: "demo", Type: typ, Domain: "demo.tainer.me"},
			Runtime: manifest.RuntimeConfig{Database: db},
			Pod:     &manifest.PodConfig{Size: manifest.PodSizeSmall},
		}
		if m.IsNode() {
			m.Runtime.Node = "24"
		} else if m.IsPHP() {
			m.Runtime.PHP = "8.4"
		}
		return m
	}
	secrets := &Secrets{DBName: "demo", DBUser: "demouser", DBPassword: "pw123", DBRootPassword: "rootpw"}

	t.Run("nodejs gets root and app env from secrets", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
			t.Fatal(err)
		}
		m := newManifest(manifest.TypeNodeJS, manifest.DatabasePostgres)
		if err := ensureProjectEnv(m, dir, secrets); err != nil {
			t.Fatalf("ensureProjectEnv: %v", err)
		}
		for _, p := range []string{".env", "app/.env"} {
			b, err := os.ReadFile(filepath.Join(dir, p))
			if err != nil {
				t.Fatalf("%s not generated: %v", p, err)
			}
			if !strings.Contains(string(b), "DB_PASSWORD=pw123") {
				t.Errorf("%s must carry the pod's secrets, got:\n%s", p, b)
			}
		}
		// app/.env is connection-only: no root password, no tainer metadata.
		app, _ := os.ReadFile(filepath.Join(dir, "app", ".env"))
		for _, forbid := range []string{"DB_ROOT_PASSWORD", "TAINER_DOMAIN"} {
			if strings.Contains(string(app), forbid) {
				t.Errorf("app/.env must not contain %s:\n%s", forbid, app)
			}
		}
	})

	t.Run("wordpress gets root env only", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
			t.Fatal(err)
		}
		m := newManifest(manifest.TypeWordPress, manifest.DatabaseMariaDB)
		if err := ensureProjectEnv(m, dir, secrets); err != nil {
			t.Fatalf("ensureProjectEnv: %v", err)
		}
		if _, err := os.ReadFile(filepath.Join(dir, ".env")); err != nil {
			t.Fatalf("root .env not generated: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "app", ".env")); err == nil {
			t.Errorf("app/.env must NOT be written for docroot types")
		}
	})

	t.Run("existing files are never overwritten", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("MINE=1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := newManifest(manifest.TypeNodeJS, manifest.DatabasePostgres)
		if err := ensureProjectEnv(m, dir, secrets); err != nil {
			t.Fatalf("ensureProjectEnv: %v", err)
		}
		b, _ := os.ReadFile(filepath.Join(dir, ".env"))
		if string(b) != "MINE=1\n" {
			t.Errorf("root .env overwritten: %s", b)
		}
		// app/.env still generated (it was missing).
		if _, err := os.Stat(filepath.Join(dir, "app", ".env")); err != nil {
			t.Errorf("app/.env should be generated when missing: %v", err)
		}
	})

	t.Run("legacy html/ is migrated before app/.env is written", func(t *testing.T) {
		dir := realDir(t)
		writeFile(t, filepath.Join(dir, "html", "package.json"), "{}")
		m := newManifest(manifest.TypeNodeJS, manifest.DatabasePostgres)
		if err := ensureProjectEnv(m, dir, secrets); err != nil {
			t.Fatalf("ensureProjectEnv: %v", err)
		}
		mustNotExist(t, filepath.Join(dir, "html"))
		mustExist(t, filepath.Join(dir, "app", "package.json"))
		mustExist(t, filepath.Join(dir, "app", ".env"))
	})

	t.Run("linked html/ is refused and no app/ is made", func(t *testing.T) {
		dir := realDir(t)
		if err := os.Symlink(realDir(t), filepath.Join(dir, "html")); err != nil {
			t.Fatal(err)
		}
		m := newManifest(manifest.TypeNodeJS, manifest.DatabasePostgres)
		if err := ensureProjectEnv(m, dir, secrets); err == nil {
			t.Fatal("expected refusal")
		}
		mustNotExist(t, filepath.Join(dir, "app"))
	})

	t.Run("nil secrets or empty dir is a no-op", func(t *testing.T) {
		m := newManifest(manifest.TypeNodeJS, manifest.DatabasePostgres)
		if err := ensureProjectEnv(m, "", secrets); err != nil {
			t.Errorf("empty dir: %v", err)
		}
		if err := ensureProjectEnv(m, t.TempDir(), nil); err != nil {
			t.Errorf("nil secrets: %v", err)
		}
	})
}

// A project's .env comes with the clone: one that is a link (say to
// ../other-project/.env) must not hand another project's credentials to
// this pod, nor may a non-regular file stand in for it. Both are refused,
// naming the file, and nothing is written to the secrets store.
func TestLoadOrCreateSecrets_RefusesLinkedProjectEnv(t *testing.T) {
	other := filepath.Join(t.TempDir(), ".env")
	body := "DB_PASSWORD=other-project-password\nDB_ROOT_PASSWORD=other-project-root\n"
	if err := os.WriteFile(other, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(dir string) error{
		"link":   func(dir string) error { return os.Symlink(other, filepath.Join(dir, ".env")) },
		"folder": func(dir string) error { return os.Mkdir(filepath.Join(dir, ".env"), 0o755) },
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := mk(dir); err != nil {
				t.Fatal(err)
			}
			secretsPath := filepath.Join(t.TempDir(), "site.env")
			s, err := LoadOrCreateSecretsAt(secretsPath, "site", dir)
			if err == nil {
				t.Fatalf("accepted a %s .env (password %q), want refused", name, s.DBPassword)
			}
			if !strings.Contains(err.Error(), filepath.Join(dir, ".env")) {
				t.Errorf("error %q does not name the file", err)
			}
			if _, err := os.Lstat(secretsPath); !os.IsNotExist(err) {
				t.Errorf("secrets store written despite refusal")
			}
		})
	}
}
