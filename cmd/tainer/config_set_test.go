package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

const configSetBase = `# shared project settings
version: 2
project:
  name: shop
  type: react
  domain: shop.tainer.me
runtime:
  node: "22"
  database: none
pod:
  size: small
`

// configSetProject writes a project folder and makes it the working directory.
func configSetProject(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, manifest.FileName)
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return path
}

type configSetDoc struct {
	V       int    `json:"v"`
	Key     string `json:"key"`
	Value   string `json:"value"`
	Changed bool   `json:"changed"`
	Note    string `json:"note"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func runConfigSet(t *testing.T, args ...string) (configSetDoc, string, int) {
	t.Helper()
	out, exit := failureHarness(t, false, func() { cmdConfig(append([]string{"set"}, args...)) })
	var doc configSetDoc
	if strings.HasPrefix(strings.TrimSpace(string(out)), "{") {
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	return doc, string(out), exit
}

func TestConfigSetWritesEveryKeyAndRoundTrips(t *testing.T) {
	for _, c := range []struct{ key, value, stored string }{
		{"pod.size", "large", "large"},
		{"database", "postgres", "postgres"},
		{"runtime.node", "24", "24"},
		{"runtime.build-dir", "./build/", "build"},
		{"live.domain", "shop.example.com", "shop.example.com"},
	} {
		t.Run(c.key, func(t *testing.T) {
			path := configSetProject(t, configSetBase)
			doc, raw, exit := runConfigSet(t, c.key, c.value, "--json")
			if exit != -1 {
				t.Fatalf("exit %d: %s", exit, raw)
			}
			if doc.V != 1 || doc.Key != c.key || doc.Value != c.stored || !doc.Changed || doc.Note != "takes effect on the next start" {
				t.Fatalf("doc: %s", raw)
			}
			m, err := manifest.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := manifest.SettingValue(m, c.key); got != c.stored {
				t.Errorf("reloaded %s = %q, want %q", c.key, got, c.stored)
			}
		})
	}
	// php is only settable on a php project.
	path := configSetProject(t, "version: 2\nproject:\n  name: blog\n  type: wordpress\n  domain: blog.tainer.me\nruntime:\n  php: \"8.3\"\n  database: mariadb\n")
	if doc, raw, exit := runConfigSet(t, "runtime.php", "8.4", "--json"); exit != -1 || !doc.Changed || doc.Value != "8.4" {
		t.Fatalf("php: exit %d %s", exit, raw)
	}
	if m, err := manifest.Load(path); err != nil || m.Runtime.PHP != "8.4" {
		t.Errorf("php reload: %v %+v", err, m)
	}
}

func TestConfigSetHumanLine(t *testing.T) {
	configSetProject(t, configSetBase)
	_, out, exit := runConfigSet(t, "pod.size", "medium")
	if exit != -1 || out != "pod.size set to medium — takes effect on the next start\n" {
		t.Errorf("exit %d, out %q", exit, out)
	}
	_, out, _ = runConfigSet(t, "pod.size", "medium")
	if out != "pod.size is already medium\n" {
		t.Errorf("no-change line %q", out)
	}
	runConfigSet(t, "live.domain", "shop.example.com")
	_, out, _ = runConfigSet(t, "live.domain", "")
	if out != "live.domain cleared — takes effect on the next start\n" {
		t.Errorf("clear line %q", out)
	}
}

func TestConfigSetSameValueDoesNotWrite(t *testing.T) {
	path := configSetProject(t, configSetBase)
	doc, raw, exit := runConfigSet(t, "pod.size", "small", "--json")
	if exit != -1 || doc.Changed || doc.Value != "small" || doc.Note != "already set" {
		t.Fatalf("exit %d: %s", exit, raw)
	}
	got, _ := os.ReadFile(path)
	if string(got) != configSetBase {
		t.Errorf("file rewritten on a no-op:\n%s", got)
	}
}

func TestConfigSetRefusesInvalidValuesWithoutWriting(t *testing.T) {
	for _, c := range []struct{ key, value, msg string }{
		{"pod.size", "huge", `invalid pod size: "huge"`},
		{"database", "mysql", `invalid database: "mysql" (expected mariadb, postgres, or none)`},
		{"live.domain", "not a domain", `live.domain: invalid domain "not a domain": use a plain hostname like mysite.tainer.me`},
		{"runtime.php", "8.3", "runtime.php applies to php projects; shop is a react project"},
	} {
		t.Run(c.key, func(t *testing.T) {
			path := configSetProject(t, configSetBase)
			doc, raw, exit := runConfigSet(t, c.key, c.value, "--json")
			if exit != 1 || doc.Error.Code != "invalid-arguments" || doc.Error.Message != c.msg {
				t.Fatalf("exit %d: %s", exit, raw)
			}
			if got, _ := os.ReadFile(path); string(got) != configSetBase {
				t.Errorf("file changed after a refused value")
			}
		})
	}
}

func TestConfigSetUnknownKeyAndBadUsage(t *testing.T) {
	configSetProject(t, configSetBase)
	doc, raw, exit := runConfigSet(t, "project.domain", "x.tainer.me", "--json")
	want := `unknown setting "project.domain" (expected one of pod.size, database, runtime.node, runtime.php, runtime.build-dir, live.domain)`
	if exit != 1 || doc.Error.Code != "invalid-arguments" || doc.Error.Message != want {
		t.Errorf("unknown key: exit %d %s", exit, raw)
	}
	doc, raw, exit = runConfigSet(t, "pod.size", "--json")
	if exit != 1 || doc.Error.Code != "invalid-arguments" || doc.Error.Message != "usage: tainer config set [project] <key> <value>" {
		t.Errorf("missing value: exit %d %s", exit, raw)
	}
	doc, raw, exit = runConfigSet(t, "pod.size", "large", "--force", "--json")
	if exit != 1 || doc.Error.Code != "invalid-arguments" {
		t.Errorf("unknown flag: exit %d %s", exit, raw)
	}
}

func TestConfigSetBrokenManifestIsManifestInvalid(t *testing.T) {
	configSetProject(t, "version: 2\nproject: [\n")
	doc, raw, exit := runConfigSet(t, "pod.size", "large", "--json")
	if exit != 1 || doc.Error.Code != "manifest-invalid" {
		t.Errorf("exit %d %s", exit, raw)
	}
}

func TestConfigSetNotesALocalOverride(t *testing.T) {
	path := configSetProject(t, configSetBase)
	overlay := filepath.Join(filepath.Dir(path), manifest.LocalOverlayName)
	if err := os.WriteFile(overlay, []byte("pod:\n  size: xxl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, raw, exit := runConfigSet(t, "pod.size", "large", "--json")
	if exit != -1 || !doc.Changed || doc.Note != "saved, but .tainer.local.yaml sets pod.size to xxl, which wins" {
		t.Fatalf("exit %d %s", exit, raw)
	}
	base, err := manifest.ParseBytes(mustRead(t, path))
	if err != nil || base.Pod.Size != manifest.PodSizeLarge {
		t.Errorf("tainer.yaml should hold large (overlay never baked in): %v %+v", err, base.Pod)
	}
}

func TestConfigSetExplicitProjectByArity(t *testing.T) {
	cwd := selfTestHome(t)
	shop := registerProject(t, t.TempDir(), "shop")

	// Four words after config: the first names the project, from any folder.
	doc, raw, exit := runConfigSet(t, "shop", "pod.size", "large", "--json")
	if exit != -1 || doc.Key != "pod.size" || doc.Value != "large" || !doc.Changed {
		t.Fatalf("explicit project: exit %d %s", exit, raw)
	}
	m, err := manifest.Load(filepath.Join(shop, manifest.FileName))
	if err != nil || m.Pod.Size != manifest.PodSizeLarge {
		t.Fatalf("shop's tainer.yaml not updated: %v %+v", err, m)
	}
	if _, err := os.Stat(filepath.Join(cwd, manifest.FileName)); err == nil {
		t.Errorf("a manifest was written in the working directory")
	}
	_, out, exit := runConfigSet(t, "shop", "runtime.node", "24")
	if exit != -1 || out != "runtime.node set to 24 — takes effect on the next start\n" {
		t.Errorf("explicit project, human: exit %d %q", exit, out)
	}

	// Three words outside any project folder: resolved from the cwd, which has none.
	doc, raw, exit = runConfigSet(t, "pod.size", "small", "--json")
	if exit != 1 || doc.Error.Code != "no-project-here" {
		t.Errorf("cwd form outside a project: exit %d %s", exit, raw)
	}

	// Three words inside the project folder: the cwd form edits that project.
	t.Chdir(filepath.Join(shop))
	doc, raw, exit = runConfigSet(t, "pod.size", "medium", "--json")
	if exit != -1 || doc.Value != "medium" || !doc.Changed {
		t.Fatalf("cwd form: exit %d %s", exit, raw)
	}
	if m, _ := manifest.Load(filepath.Join(shop, manifest.FileName)); m == nil || m.Pod.Size != manifest.PodSizeMedium {
		t.Errorf("cwd form did not update shop: %+v", m)
	}
}

func TestConfigSetUnknownProject(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "shop")
	doc, raw, exit := runConfigSet(t, "nope", "pod.size", "large", "--json")
	want := `unknown project "nope" (run ` + "`tainer list`" + ` to see registered projects)`
	if exit != 1 || doc.Error.Code != "project-not-found" || doc.Error.Message != want {
		t.Errorf("exit %d %s", exit, raw)
	}
	// Too many words is bad usage, not a lookup.
	doc, raw, exit = runConfigSet(t, "shop", "pod.size", "large", "extra", "--json")
	if exit != 1 || doc.Error.Code != "invalid-arguments" {
		t.Errorf("five words: exit %d %s", exit, raw)
	}
}

func TestConfigSetAcceptsRuntimeDatabaseAndEchoesDatabase(t *testing.T) {
	configSetProject(t, configSetBase)
	doc, raw, exit := runConfigSet(t, "runtime.database", "postgres", "--json")
	if exit != -1 || doc.Key != "database" || doc.Value != "postgres" || !doc.Changed {
		t.Fatalf("exit %d %s", exit, raw)
	}
	_, out, _ := runConfigSet(t, "runtime.database", "postgres")
	if out != "database is already postgres\n" {
		t.Errorf("human %q", out)
	}
}

func TestConfigSetMissingValueHintsAtTheProjectForm(t *testing.T) {
	configSetProject(t, configSetBase)
	doc, raw, exit := runConfigSet(t, "shop", "pod.size", "--json")
	want := "missing the value for pod.size (to change it for project shop: tainer config set shop pod.size <value>)"
	if exit != 1 || doc.Error.Code != "invalid-arguments" || doc.Error.Message != want {
		t.Errorf("exit %d %s", exit, raw)
	}
}

func TestConfigSetKeepsCommentsUnknownKeysAndMode(t *testing.T) {
	src := "# shop settings\nversion: 2\nproject:\n  name: shop\n  type: react\n  domain: shop.tainer.me\n  custom-thing: 1\nruntime:\n  node: \"22\"\n  database: none\npod:\n  size: small\nscripts:\n  seed: npm run seed\n"
	path := configSetProject(t, src)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, raw, exit := runConfigSet(t, "pod.size", "large", "--json"); exit != -1 {
		t.Fatalf("exit %d %s", exit, raw)
	}
	got := string(mustRead(t, path))
	if want := strings.Replace(src, "size: small", "size: large", 1); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
}

func TestConfigSetSaysSoWhenTheProjectNoLongerLoads(t *testing.T) {
	custom := strings.Replace(configSetBase, "  size: small\n", "  size: custom\n  memory: 3G\n  cpu: 2\n", 1)
	path := configSetProject(t, custom)
	// The overlay keeps size custom; the shared file losing its budget then breaks the merge.
	overlay := filepath.Join(filepath.Dir(path), manifest.LocalOverlayName)
	if err := os.WriteFile(overlay, []byte("pod:\n  size: custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, raw, exit := runConfigSet(t, "pod.size", "medium", "--json")
	if exit != -1 || !doc.Changed || !strings.HasPrefix(doc.Note, "saved, but the project no longer loads: ") || !strings.Contains(doc.Note, "pod.size=custom requires pod.memory and pod.cpu") {
		t.Fatalf("exit %d %s", exit, raw)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestJSONSnapshotConfigSet(t *testing.T) {
	assertSnapshot(t, "config-set", func() {
		emitConfigSetJSON(configSetResult{Key: "pod.size", Value: "large", Changed: true, Note: configSetNoteNextStart})
	})
	assertSnapshot(t, "config-set-nochange", func() {
		emitConfigSetJSON(configSetResult{Key: "pod.size", Value: "small", Note: configSetNoteAlreadySet})
	})
}
