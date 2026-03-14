package env

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomPassword(length int) string {
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// Generate creates a .env file for the project. If the file already exists, it is not overwritten.
func Generate(m *manifest.Manifest, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // file exists, skip
	}

	var lines []string

	if m.HasDatabase() {
		dbPassword := randomPassword(32)
		rootPassword := randomPassword(32)
		lines = append(lines,
			"# Database (generic)",
			"DB_HOST=127.0.0.1",
			fmt.Sprintf("DB_PORT=%s", m.DBPort()),
			"DB_NAME=tainer",
			"DB_USER=tainer",
			fmt.Sprintf("DB_PASSWORD=%s", dbPassword),
			fmt.Sprintf("DB_ROOT_PASSWORD=%s", rootPassword),
		)

		// Native env vars for DB container initialization
		if m.Runtime.Database == manifest.DatabaseMariaDB {
			lines = append(lines,
				"",
				"# MariaDB native vars",
				"MYSQL_DATABASE=tainer",
				"MYSQL_USER=tainer",
				fmt.Sprintf("MYSQL_PASSWORD=%s", dbPassword),
				fmt.Sprintf("MYSQL_ROOT_PASSWORD=%s", rootPassword),
			)
		} else if m.Runtime.Database == manifest.DatabasePostgres {
			lines = append(lines,
				"",
				"# PostgreSQL native vars",
				"POSTGRES_DB=tainer",
				"POSTGRES_USER=tainer",
				fmt.Sprintf("POSTGRES_PASSWORD=%s", dbPassword),
			)
		}

		if m.IsNode() {
			var scheme string
			if m.Runtime.Database == manifest.DatabasePostgres {
				scheme = "postgresql"
			} else {
				scheme = "mysql"
			}
			lines = append(lines,
				"",
				fmt.Sprintf("DATABASE_URL=%s://tainer:%s@127.0.0.1:%s/tainer", scheme, dbPassword, m.DBPort()),
			)
		}
	}

	if m.Project.Type == manifest.TypeWordPress {
		lines = append(lines,
			"",
			"# WordPress",
			"WP_DEBUG=true",
			fmt.Sprintf("WP_HOME=https://%s", m.Project.Domain),
			fmt.Sprintf("WP_SITEURL=https://%s", m.Project.Domain),
		)
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}
