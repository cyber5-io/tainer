package pod

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Secrets holds per-project credentials shared between containers in a pod.
// Loaded from ~/.cyberstack/tainer/secrets/<project>.env on every Start;
// generated and persisted on first Start.
//
// The file is plaintext `KEY=value` lines, mode 0600. Values are URL-safe
// base64 (~24 chars from 18 random bytes, no shell-special characters so
// they pass through env unescaped).
type Secrets struct {
	DBName         string
	DBUser         string
	DBPassword     string
	DBRootPassword string
}

// SecretsDir returns the canonical secrets directory.
func SecretsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cyberstack", "tainer", "secrets")
}

// SecretsPath returns the env file path for a given project.
func SecretsPath(project string) string {
	return filepath.Join(SecretsDir(), project+".env")
}

// LoadOrCreateSecrets returns the secrets for project, generating + persisting
// them on first call. The DB name and user default to "tainer" — only the
// passwords are randomised.
func LoadOrCreateSecrets(project, projectDir string) (*Secrets, error) {
	return LoadOrCreateSecretsAt(SecretsPath(project), project, projectDir)
}

// LoadOrCreateSecretsAt is LoadOrCreateSecrets with an explicit path (for tests).
//
// When no secret exists yet, it first tries to adopt DB credentials from the
// project's .env (projectDir/.env). An old project (created before this
// secrets store existed) already has its database initialized with the
// passwords in its .env; minting fresh random ones here would never match that
// on-disk DB and would lock the app out (auth failure the app reports as
// "database not reachable"). Only a project with no adoptable .env creds — a
// genuinely new one — gets freshly generated passwords.
func LoadOrCreateSecretsAt(path, project, projectDir string) (*Secrets, error) {
	s, err := loadSecrets(path)
	if err == nil {
		return s, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	// Adopt existing DB credentials from the project's .env if present and
	// complete (loadSecrets requires both DB passwords). The .env uses the
	// same KEY=value shape and key names as the secrets file.
	if projectDir != "" {
		if seeded, serr := loadSecrets(filepath.Join(projectDir, ".env")); serr == nil {
			if err := writeSecrets(path, seeded); err != nil {
				return nil, err
			}
			return seeded, nil
		}
	}

	s = &Secrets{
		DBName:         "tainer",
		DBUser:         "tainer",
		DBPassword:     mustRandPassword(),
		DBRootPassword: mustRandPassword(),
	}
	if err := writeSecrets(path, s); err != nil {
		return nil, err
	}
	return s, nil
}

func loadSecrets(path string) (*Secrets, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := &Secrets{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "DB_NAME":
			out.DBName = v
		case "DB_USER":
			out.DBUser = v
		case "DB_PASSWORD":
			out.DBPassword = v
		case "DB_ROOT_PASSWORD":
			out.DBRootPassword = v
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if out.DBPassword == "" || out.DBRootPassword == "" {
		return nil, fmt.Errorf("secrets file %s missing required keys", path)
	}
	if out.DBName == "" {
		out.DBName = "tainer"
	}
	if out.DBUser == "" {
		out.DBUser = "tainer"
	}
	return out, nil
}

func writeSecrets(path string, s *Secrets) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	body := fmt.Sprintf(
		"DB_NAME=%s\nDB_USER=%s\nDB_PASSWORD=%s\nDB_ROOT_PASSWORD=%s\n",
		s.DBName, s.DBUser, s.DBPassword, s.DBRootPassword,
	)
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// mustRandPassword returns a 24-char URL-safe random string. Panics on
// crypto/rand failure (which is "world is broken" territory on supported
// platforms — not something to surface to callers as a recoverable error).
func mustRandPassword() string {
	b := make([]byte, 18) // 18 bytes → 24 URL-safe base64 chars
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("tainer: crypto/rand failed: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
