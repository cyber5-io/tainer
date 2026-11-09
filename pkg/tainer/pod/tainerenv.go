package pod

import (
	"bufio"
	"fmt"
	"io"
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
		set, err := envFileSets(filepath.Join(projectDir, filepath.FromSlash(rel)), rel, tainerOwnedEnv)
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
// KEY=value or `export KEY=value` form. Lines of any length are fine: only
// the start of each line is looked at, so a long blob (a certificate, a
// base64 value) never stops the check. rel names the file in errors.
func envFileSets(path, rel, key string) (bool, error) {
	f, err := openNoFollow(path)
	if err != nil {
		// Missing, or a link or other non-regular file that tainer never
		// reads through: nothing of the project's own sets the key.
		return false, nil
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		head, more, err := r.ReadLine()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("read %s: %w", rel, err)
		}
		set := lineSets(string(head), key)
		// Drain the rest of an over-long line; the key sits at its start.
		for more {
			if _, more, err = r.ReadLine(); err != nil && err != io.EOF {
				return false, fmt.Errorf("read %s: %w", rel, err)
			}
			if err == io.EOF {
				break
			}
		}
		if set {
			return true, nil
		}
	}
}

// lineSets reports whether one dotenv line assigns key.
func lineSets(line, key string) bool {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return false
	}
	if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		line = strings.TrimSpace(rest)
	}
	k, _, ok := strings.Cut(line, "=")
	return ok && strings.TrimSpace(k) == key
}
