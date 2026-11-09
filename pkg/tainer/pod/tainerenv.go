package pod

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// tainerOwnedEnv is the one variable a project may not set itself: the
// local binary always runs pods with TAINER_ENV=dev, and a hosting server's
// binary owns live/staging. Only this key — TAINER_DOMAIN is written into
// the generated .env by tainer itself.
const tainerOwnedEnv = "TAINER_ENV"

// checkTainerEnv refuses a project whose root .env or app/.env sets
// TAINER_ENV. The files are scanned for that key only, never loaded. A
// file that is missing, or not a real file of the project's own (a link),
// is not read.
func checkTainerEnv(m *manifest.Manifest, projectDir string) error {
	for _, rel := range []string{".env", m.HostAppDir() + "/.env"} {
		set, err := envFileSets(filepath.Join(projectDir, filepath.FromSlash(rel)), tainerOwnedEnv)
		if err != nil {
			return err
		}
		if set {
			return fmt.Errorf("%s is set by tainer — remove it from %s", tainerOwnedEnv, rel)
		}
	}
	return nil
}

// envFileSets reports whether the dotenv file at path assigns key, in the
// KEY=value or `export KEY=value` form.
func envFileSets(path, key string) (bool, error) {
	f, err := openNoFollow(path)
	if err != nil {
		// Missing, or a link or other non-regular file that tainer never
		// reads through: nothing of the project's own sets the key.
		return false, nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		k, _, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return true, nil
		}
	}
	if err := sc.Err(); err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return false, nil
}
