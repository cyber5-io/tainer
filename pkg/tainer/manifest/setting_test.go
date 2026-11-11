package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wpYAML = `version: 2
project:
  name: blog
  type: wordpress
  domain: blog.tainer.me
runtime:
  php: "8.3"
  database: mariadb
pod:
  size: small
`

const nodeYAML = `version: 2
project:
  name: api
  type: nodejs
  domain: api.tainer.me
runtime:
  node: "22"
  database: none
`

const reactYAML = `version: 2
project:
  name: shop
  type: react
  domain: shop.tainer.me
runtime:
  node: "22"
  database: none
live:
  domain: shop.example.com
`

const customYAML = `version: 2
project:
  name: big
  type: nodejs
  domain: big.tainer.me
runtime:
  node: "22"
  database: none
pod:
  size: custom
  memory: 3G
  cpu: 2
`

func TestApplySettingEveryKey(t *testing.T) {
	cases := []struct {
		name, base, key, value, stored string
	}{
		{"pod size", wpYAML, "pod.size", "large", "large"},
		{"database", wpYAML, "database", "postgres", "postgres"},
		{"database alias", wpYAML, "runtime.database", "postgres", "postgres"},
		{"database none", wpYAML, "database", "none", "none"},
		{"php", wpYAML, "runtime.php", "8.4", "8.4"},
		{"node", nodeYAML, "runtime.node", "24", "24"},
		{"build dir", reactYAML, "runtime.build-dir", "./build/", "build"},
		{"build dir nested", reactYAML, "runtime.build-dir", "web/out", "web/out"},
		{"live domain new", wpYAML, "live.domain", "blog.example.com", "blog.example.com"},
		{"live domain change", reactYAML, "live.domain", "www.shop.example.com", "www.shop.example.com"},
		{"live domain clear", reactYAML, "live.domain", "", ""},
		{"custom to named", customYAML, "pod.size", "medium", "medium"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, stored, changed, err := ApplySetting([]byte(c.base), c.key, c.value)
			if err != nil {
				t.Fatalf("ApplySetting: %v", err)
			}
			m := mustParse(t, out)
			if !changed || stored != c.stored {
				t.Fatalf("changed=%v stored=%q, want true %q", changed, stored, c.stored)
			}
			if got, _ := SettingValue(m, c.key); got != c.stored {
				t.Fatalf("manifest holds %q, want %q", got, c.stored)
			}
		})
	}
}

func TestApplySettingClearsCustomBudgetAndLiveSection(t *testing.T) {
	out, _, _, err := ApplySetting([]byte(customYAML), "pod.size", "medium")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "memory") || strings.Contains(string(out), "cpu") {
		t.Errorf("custom budget lines kept:\n%s", out)
	}
	m := mustParse(t, out)
	if m.Pod.Memory != "" || m.Pod.CPU != 0 {
		t.Errorf("custom budget kept on a named size: %+v", m.Pod)
	}
	out, _, _, err = ApplySetting([]byte(reactYAML), "live.domain", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "live") {
		t.Errorf("live section kept:\n%s", out)
	}
	if m = mustParse(t, out); m.Live != nil {
		t.Errorf("clearing live.domain should drop the live section, got %+v", m.Live)
	}
}

func TestApplySettingNoChange(t *testing.T) {
	cases := []struct{ base, key, value string }{
		{wpYAML, "pod.size", "small"},
		{nodeYAML, "pod.size", "small"}, // default size
		{wpYAML, "database", "mariadb"},
		{wpYAML, "runtime.database", "mariadb"},
		{wpYAML, "runtime.php", "8.3"},
		{nodeYAML, "runtime.node", "22"},
		{reactYAML, "live.domain", "shop.example.com"},
		{wpYAML, "live.domain", ""},
		{reactYAML, "runtime.build-dir", ""},
	}
	for _, c := range cases {
		out, stored, changed, err := ApplySetting([]byte(c.base), c.key, c.value)
		if err != nil {
			t.Fatalf("%s=%q: %v", c.key, c.value, err)
		}
		if string(out) != c.base {
			t.Errorf("%s=%q: no-change output differs from the input", c.key, c.value)
		}
		if changed {
			t.Errorf("%s=%q reported a change (stored %q)", c.key, c.value, stored)
		}
	}
	// A spelling that normalises to the current value is not a change either.
	withDir := strings.Replace(reactYAML, "  database: none\n", "  database: none\n  build-dir: dist\n", 1)
	if _, _, changed, err := ApplySetting([]byte(withDir), "runtime.build-dir", "./dist/"); err != nil || changed {
		t.Errorf("./dist/ over dist: changed=%v err=%v", changed, err)
	}
}

func TestApplySettingInvalidValues(t *testing.T) {
	cases := []struct{ name, base, key, value, want string }{
		{"size", wpYAML, "pod.size", "huge", `invalid pod size: "huge"`},
		{"custom without budget", wpYAML, "pod.size", "custom", "pod.size=custom requires pod.memory and pod.cpu"},
		{"database", wpYAML, "database", "mysql", `invalid database: "mysql" (expected mariadb, postgres, or none)`},
		{"live domain", wpYAML, "live.domain", "bad domain!", `live.domain: invalid domain "bad domain!"`},
		{"build dir escape", reactYAML, "runtime.build-dir", "../out", "must stay inside the project"},
		{"build dir absolute", reactYAML, "runtime.build-dir", "/srv/out", "not an absolute path"},
		{"php empty", wpYAML, "runtime.php", "", "php version required for wordpress projects"},
		{"node empty", nodeYAML, "runtime.node", "", "node version required for nodejs projects"},
		{"php junk", wpYAML, "runtime.php", "8.3; rm -rf /", `runtime.php "8.3; rm -rf /": use a version number like 8.3`},
		{"node junk", nodeYAML, "runtime.node", "latest", `runtime.node "latest": use a version number like 22`},
		{"php on node", nodeYAML, "runtime.php", "8.3", "runtime.php applies to php projects; api is a nodejs project"},
		{"node on php", wpYAML, "runtime.node", "22", "runtime.node applies to node projects; blog is a wordpress project"},
		{"build dir on php", wpYAML, "runtime.build-dir", "dist", "runtime.build-dir applies to react and svelte projects; blog is a wordpress project"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, _, err := ApplySetting([]byte(c.base), c.key, c.value)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
			var ie *InvalidSettingError
			if !errors.As(err, &ie) {
				t.Errorf("want an InvalidSettingError, got %T", err)
			}
		})
	}
}

func TestApplySettingUnknownKeyListsTheAllowedOnes(t *testing.T) {
	_, _, _, err := ApplySetting([]byte(wpYAML), "project.name", "x")
	var uk *UnknownSettingError
	if !errors.As(err, &uk) {
		t.Fatalf("want UnknownSettingError, got %v", err)
	}
	want := "unknown setting \"project.name\" (expected one of pod.size, database, runtime.node, runtime.php, runtime.build-dir, live.domain)"
	if err.Error() != want {
		t.Errorf("got  %q\nwant %q", err, want)
	}
}

func TestApplySettingRefusesABrokenFile(t *testing.T) {
	_, _, _, err := ApplySetting([]byte("version: 2\nproject: [\n"), "pod.size", "large")
	var ie *InvalidSettingError
	if err == nil || errors.As(err, &ie) {
		t.Fatalf("a manifest that does not parse is not an invalid value: %v", err)
	}
}

func TestApplySettingRoundTripsThroughSaveAndLoad(t *testing.T) {
	for _, c := range []struct{ base, key, value string }{
		{wpYAML, "pod.size", "xlarge"},
		{wpYAML, "database", "postgres"},
		{wpYAML, "runtime.php", "8.4"},
		{nodeYAML, "runtime.node", "24"},
		{reactYAML, "runtime.build-dir", "build"},
		{wpYAML, "live.domain", "blog.example.com"},
		{reactYAML, "live.domain", ""},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, FileName)
		out, stored, _, err := ApplySetting([]byte(c.base), c.key, c.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("%s: reload: %v", c.key, err)
		}
		if got, _ := SettingValue(loaded, c.key); got != stored {
			t.Errorf("%s: reloaded %q, want %q", c.key, got, stored)
		}
		if loaded.Project.Name == "" || loaded.Project.Domain == "" {
			t.Errorf("%s: other fields lost: %+v", c.key, loaded.Project)
		}
	}
}

func mustParse(t *testing.T, raw []byte) *Manifest {
	t.Helper()
	m, err := ParseBytes(raw)
	if err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	return m
}

// A hand-written file: a header comment, a key tainer doesn't know under
// project:, a section it doesn't know, an inline comment, 4-space indent.
const handWrittenYAML = `# shop: shared settings, edit with care
version: 2
project:
    name: shop
    type: react
    domain: shop.tainer.me
    custom-thing: 1
runtime:
    node: "22" # matches CI
    database: none
pod:
    size: small
scripts:
    seed: npm run seed
    lint: npm run lint
`

func TestApplySettingPreservesTheRestOfTheFile(t *testing.T) {
	for _, c := range []struct{ key, value, line string }{
		{"pod.size", "large", "    size: large"},
		{"database", "postgres", "    database: postgres"},
		{"runtime.node", "24", `    node: "24" # matches CI`},
		{"runtime.build-dir", "build", "    build-dir: build"},
		{"live.domain", "shop.example.com", "    domain: shop.example.com"},
	} {
		t.Run(c.key, func(t *testing.T) {
			out, _, changed, err := ApplySetting([]byte(handWrittenYAML), c.key, c.value)
			if err != nil || !changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			got := string(out)
			for _, keep := range []string{
				"# shop: shared settings, edit with care\n",
				"    custom-thing: 1\n",
				"scripts:\n    seed: npm run seed\n    lint: npm run lint\n",
				"# matches CI",
			} {
				if !strings.Contains(got, keep) {
					t.Errorf("lost %q:\n%s", keep, got)
				}
			}
			if !strings.Contains(got, c.line+"\n") {
				t.Errorf("want line %q:\n%s", c.line, got)
			}
			// Only the edited line differs (a new key adds one or two lines).
			before, after := strings.Split(handWrittenYAML, "\n"), strings.Split(got, "\n")
			kept := 0
			for _, l := range before {
				if strings.Contains(got, l+"\n") || l == "" {
					kept++
				}
			}
			if len(before)-kept > 1 || len(after)-len(before) > 2 {
				t.Errorf("more than the target changed (%d lines lost, %d added):\n%s", len(before)-kept, len(after)-len(before), got)
			}
			if got, _ := SettingValue(mustParse(t, out), c.key); got != c.value {
				t.Errorf("reparsed %s = %q", c.key, got)
			}
		})
	}
}

func TestApplySettingKeepsTwoSpaceIndent(t *testing.T) {
	out, _, _, err := ApplySetting([]byte(wpYAML), "pod.size", "large")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(wpYAML, "  size: small", "  size: large", 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestApplySettingOnAVersion1File(t *testing.T) {
	v1 := "version: 1\nproject:\n  name: old\n  type: nodejs\n  domain: old.tainer.me\nruntime:\n  node: \"20\"\n  database: none\n"
	out, _, _, err := ApplySetting([]byte(v1), "runtime.node", "22")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "version: 1\n") || !strings.Contains(string(out), `node: "22"`) {
		t.Errorf("v1 file should be edited in place, not migrated:\n%s", out)
	}
	out, _, _, err = ApplySetting([]byte(v1), "live.domain", "old.example.com")
	if err != nil || !strings.HasPrefix(string(out), "version: 1\n") {
		t.Errorf("live.domain on v1: %v\n%s", err, out)
	}
}
