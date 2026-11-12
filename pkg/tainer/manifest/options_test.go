package manifest

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func mustOptions(t *testing.T, yaml string) Options {
	t.Helper()
	m, err := ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return OptionsFor(m)
}

func TestOptionsSizesAreTheSixNamedBudgets(t *testing.T) {
	o := mustOptions(t, wpYAML)
	want := []SizeOption{
		{"nano", "512M", 1}, {"small", "1G", 1}, {"medium", "2G", 2},
		{"large", "4G", 4}, {"xlarge", "8G", 6}, {"xxl", "16G", 8},
	}
	if !reflect.DeepEqual(o.Size.Values, want) {
		t.Errorf("sizes %+v", o.Size.Values)
	}
	if o.Size.Key != "pod.size" || o.Size.Current != "small" || o.Size.Default != "small" {
		t.Errorf("size %+v", o.Size)
	}
}

func TestOptionsPerType(t *testing.T) {
	cases := []struct {
		name, yaml           string
		dbs                  []DatabaseType
		dbDefault, dbCurrent DatabaseType
		rtKey, rtName        string
		rtValues             []string
		rtCurrent, rtDefault string
		buildApplies         bool
		buildCurrent         string
	}{
		{"wordpress", wpYAML, []DatabaseType{"mariadb"}, "mariadb", "mariadb",
			"runtime.php", "php", PHPVersions, "8.3", "8.3", false, ""},
		{"php", strings.Replace(wpYAML, "type: wordpress", "type: php", 1), []DatabaseType{"mariadb", "postgres", "none"}, "mariadb", "mariadb",
			"runtime.php", "php", PHPVersions, "8.3", "8.3", false, ""},
		{"nodejs", nodeYAML, []DatabaseType{"none", "postgres", "mariadb"}, "none", "none",
			"runtime.node", "node", NodeVersions, "22", "22", false, ""},
		{"react", reactYAML, []DatabaseType{"none", "postgres", "mariadb"}, "none", "none",
			"runtime.node", "node", NodeVersions, "22", "22", true, "dist"},
		{"kompozi", strings.Replace(strings.Replace(nodeYAML, "type: nodejs", "type: kompozi", 1), "database: none", "database: postgres", 1),
			[]DatabaseType{"postgres", "mariadb"}, "postgres", "postgres", "runtime.node", "node", NodeVersions, "22", "22", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := mustOptions(t, c.yaml)
			if o.Database.Key != "database" || !reflect.DeepEqual(o.Database.Values, c.dbs) || o.Database.Default != c.dbDefault || o.Database.Current != c.dbCurrent {
				t.Errorf("database %+v", o.Database)
			}
			if o.Runtime.Key != c.rtKey || o.Runtime.Name != c.rtName || !reflect.DeepEqual(o.Runtime.Values, c.rtValues) ||
				o.Runtime.Current != c.rtCurrent || o.Runtime.Default != c.rtDefault {
				t.Errorf("runtime %+v", o.Runtime)
			}
			if o.BuildDir.Key != "runtime.build-dir" || o.BuildDir.Applies != c.buildApplies || o.BuildDir.Current != c.buildCurrent {
				t.Errorf("buildDir %+v", o.BuildDir)
			}
			if c.buildApplies && o.BuildDir.Default != "dist" {
				t.Errorf("buildDir default %q", o.BuildDir.Default)
			}
		})
	}
}

func TestOptionsShowAChosenBuildDir(t *testing.T) {
	o := mustOptions(t, strings.Replace(reactYAML, "  database: none\n", "  database: none\n  build-dir: build\n", 1))
	if o.BuildDir.Current != "build" || o.BuildDir.Default != "dist" {
		t.Errorf("%+v", o.BuildDir)
	}
}

func TestOptionsEverySettableValueIsAccepted(t *testing.T) {
	// The options and config set must agree: every offered value sets cleanly.
	for _, base := range []string{wpYAML, nodeYAML, reactYAML} {
		o := mustOptions(t, base)
		for _, s := range o.Size.Values {
			if _, _, _, err := ApplySetting([]byte(base), o.Size.Key, string(s.Name)); err != nil {
				t.Errorf("%s %s: %v", o.Type, s.Name, err)
			}
		}
		for _, d := range o.Database.Values {
			if _, _, _, err := ApplySetting([]byte(base), o.Database.Key, string(d)); err != nil {
				t.Errorf("%s %s: %v", o.Type, d, err)
			}
		}
		for _, v := range o.Runtime.Values {
			if _, _, _, err := ApplySetting([]byte(base), o.Runtime.Key, v); err != nil {
				t.Errorf("%s %s=%s: %v", o.Type, o.Runtime.Key, v, err)
			}
		}
	}
}

func TestOptionsJSONKeys(t *testing.T) {
	raw, err := json.Marshal(mustOptions(t, reactYAML))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	for _, k := range []string{"project", "type", "size", "database", "runtime", "buildDir"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("missing %q in %s", k, raw)
		}
	}
}
