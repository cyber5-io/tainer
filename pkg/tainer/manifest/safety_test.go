package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProject writes a tainer.yaml (and an optional overlay) into a fresh
// temp dir and returns the manifest path.
func writeProject(t *testing.T, base, overlay string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if overlay != "" {
		if err := os.WriteFile(filepath.Join(dir, LocalOverlayName), []byte(overlay), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

const safeBase = `version: 2
project: { name: site, type: wordpress, domain: site.tainer.me }
runtime: { php: "8.4", database: mariadb }
pod: { size: small }
`

func TestMountNamesMustBeOnePlainFolder(t *testing.T) {
	bad := []string{"", ".", "..", ".git", ".ssh", "a/b", "/etc", "../x", "x/..", "html", "data", "db", "a\\b", "a\x00b"}
	for _, name := range bad {
		m := &Manifest{Version: 2, Project: ProjectConfig{Name: "site", Type: TypeWordPress}, Runtime: RuntimeConfig{PHP: "8.4", Database: DatabaseMariaDB}, Mounts: []string{name}}
		m.defaults()
		if err := m.validate(); err == nil {
			t.Errorf("mount %q: accepted, want refused", name)
		}
	}
	for _, name := range []string{"media", "uploads", "my-files", "assets_2", "v1.2"} {
		m := &Manifest{Version: 2, Project: ProjectConfig{Name: "site", Type: TypeWordPress}, Runtime: RuntimeConfig{PHP: "8.4", Database: DatabaseMariaDB}, Mounts: []string{name}}
		m.defaults()
		if err := m.validate(); err != nil {
			t.Errorf("mount %q: refused (%v), want accepted", name, err)
		}
	}
}

func TestOverlayCannotBypassValidation(t *testing.T) {
	cases := map[string]string{
		"mount traversal":   "mounts: [\"../../.ssh\"]\n",
		"dot mount":         "mounts: [\".git\"]\n",
		"project name":      "project: { name: \"../../x\" }\n",
		"project type":      "project: { type: evil }\n",
		"domain injection":  "project: { domain: \"x.test {\\n}\" }\n",
		"host port band":    "ports: [{ role: app, container: 22, protocol: tcp, host_port: 22 }]\n",
		"build-dir escape":  "runtime: { build-dir: \"../../..\" }\n",
		"database":          "runtime: { database: oracle }\n",
		"pod size":          "pod: { size: huge }\n",
		"absolute builddir": "runtime: { build-dir: /etc }\n",
	}
	for name, ov := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeProject(t, safeBase, ov)
			if _, err := Load(path); err == nil {
				t.Errorf("overlay %q accepted, want refused", ov)
			}
		})
	}
}

func TestOverlayLegitStillLoads(t *testing.T) {
	path := writeProject(t, safeBase, "mounts: [media]\npod: { size: large }\nproject: { domain: other.test }\n")
	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.Mounts) != 1 || m.Mounts[0] != "media" || m.Pod.Size != PodSizeLarge || m.Project.Domain != "other.test" {
		t.Errorf("overlay not applied: %+v", m)
	}
}

func TestBuildDirStaysInsideProject(t *testing.T) {
	base := func(dir string) string {
		return "version: 2\nproject: { name: spa, type: react, domain: spa.tainer.me }\nruntime: { node: \"22\", database: none, build-dir: \"" + dir + "\" }\n"
	}
	for _, dir := range []string{"..", "../..", "../../etc", "/etc", "a/../../b", "a/../b", "dist\n}", "dist {", "dist }", "$HOME", "{$X}", "~/x"} {
		if _, err := ParseBytes([]byte(base(dir))); err == nil {
			t.Errorf("build-dir %q accepted, want refused", dir)
		}
	}
	for dir, want := range map[string]string{"dist": "dist", "build": "build", "out/client": "out/client", "dist/": "dist", "./build": "build", "": ""} {
		m, err := ParseBytes([]byte(base(dir)))
		if err != nil {
			t.Errorf("build-dir %q refused (%v), want accepted", dir, err)
			continue
		}
		if m.Runtime.BuildDir != want {
			t.Errorf("build-dir %q normalised to %q, want %q", dir, m.Runtime.BuildDir, want)
		}
	}
}

func TestDomainMustBeAHostname(t *testing.T) {
	base := func(d string) string {
		return "version: 2\nproject: { name: site, type: wordpress, domain: \"" + d + "\" }\nruntime: { php: \"8.4\", database: mariadb }\n"
	}
	for _, d := range []string{"a b.test", "x.test {", "x.test\n:80", "-x.test", "x..test", "x/y.test", strings.Repeat("a", 64) + ".test"} {
		if _, err := ParseBytes([]byte(base(d))); err == nil {
			t.Errorf("domain %q accepted, want refused", d)
		}
	}
	for _, d := range []string{"site.tainer.me", "my-client.tainer.me", "myapp.test", "localhost", "Site.Test"} {
		if _, err := ParseBytes([]byte(base(d))); err != nil {
			t.Errorf("domain %q refused (%v), want accepted", d, err)
		}
	}
}

// On a case-insensitive disk (APFS default) "DB" is the db folder, so the
// reserved names and duplicates are compared ignoring case (T4), and mount
// names use the same plain characters as build-dir (T5).
func TestMountNamesIgnoreCaseAndUsePlainCharacters(t *testing.T) {
	refused := [][]string{
		{"DB"}, {"Data"}, {"HTML"}, {"hTmL"},
		{"media", "Media"}, {"uploads", "uploads"},
		{"my files"}, {"a{b}"}, {"$HOME"}, {"x'y"}, {"tab\tname"}, {"café"}, {"a:b"},
	}
	for _, mounts := range refused {
		m := &Manifest{Version: 2, Project: ProjectConfig{Name: "site", Type: TypeWordPress}, Runtime: RuntimeConfig{PHP: "8.4", Database: DatabaseMariaDB}, Mounts: mounts}
		m.defaults()
		if err := m.validate(); err == nil {
			t.Errorf("mounts %q: accepted, want refused", mounts)
		}
	}
	m := &Manifest{Version: 2, Project: ProjectConfig{Name: "site", Type: TypeWordPress}, Runtime: RuntimeConfig{PHP: "8.4", Database: DatabaseMariaDB}, Mounts: []string{"Media", "uploads", "A-b_c.1"}}
	m.defaults()
	if err := m.validate(); err != nil {
		t.Errorf("plain mounts refused: %v", err)
	}
}
