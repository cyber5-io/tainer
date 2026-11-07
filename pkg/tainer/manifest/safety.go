package manifest

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Project files (tainer.yaml, .tainer.local.yaml) arrive with a clone, so
// every value tainer turns into a host path, a config line or a container
// path is checked here, lexically. The on-disk checks (links, folders that
// resolve outside the project) happen when the mounts are built.

// reservedMounts are the folders tainer itself mounts.
var reservedMounts = map[string]bool{"html": true, "data": true, "db": true}

// validateMountName accepts exactly one plain folder name directly inside
// the project: not empty, no separators, not "." or "..", no leading dot
// (".git", ".ssh", ".env" stay private), and not one tainer reserves.
func validateMountName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("mount name cannot be empty")
	case strings.ContainsAny(name, "/\\\x00"), name == ".", name == "..":
		return fmt.Errorf("mount name must be a simple folder name inside the project: %q", name)
	case strings.HasPrefix(name, "."):
		return fmt.Errorf("mount name %q cannot start with a dot", name)
	case reservedMounts[name]:
		return fmt.Errorf("mount name %q is reserved", name)
	}
	return nil
}

// buildDirPattern is the character set a build-dir may use. The value is
// substituted into the web server's config, so nothing that config syntax
// treats specially ({ } $ whitespace quotes) gets through.
var buildDirPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// cleanBuildDir checks runtime.build-dir (a folder inside html/) and returns
// it cleaned: relative, no "..", and only plain path characters.
func cleanBuildDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	if !buildDirPattern.MatchString(dir) {
		return "", fmt.Errorf("runtime.build-dir %q: use only letters, numbers, '.', '_', '-' and '/'", dir)
	}
	if strings.HasPrefix(dir, "/") {
		return "", fmt.Errorf("runtime.build-dir %q must be a folder inside the project, not an absolute path", dir)
	}
	for _, el := range strings.Split(dir, "/") {
		if el == ".." {
			return "", fmt.Errorf("runtime.build-dir %q must stay inside the project (no '..')", dir)
		}
	}
	return path.Clean(dir), nil
}

var domainLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// validateDomain accepts an empty domain or a plain hostname. The domain is
// written into the router's config, so anything else is refused.
func validateDomain(d string) error {
	if d == "" {
		return nil
	}
	if len(d) > 253 {
		return fmt.Errorf("invalid domain %q: too long", d)
	}
	for _, label := range strings.Split(d, ".") {
		if !domainLabel.MatchString(label) {
			return fmt.Errorf("invalid domain %q: use a plain hostname like mysite.tainer.me", d)
		}
	}
	return nil
}
