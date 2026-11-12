package main

import (
	"encoding/json"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// tain-127: `tainer config options [project]` lists what can be set.

func runConfigOptions(t *testing.T, args ...string) (map[string]any, string, int) {
	t.Helper()
	out, exit := failureHarness(t, false, func() { cmdConfig(append([]string{"options"}, args...)) })
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	return doc, string(out), exit
}

func TestConfigOptionsJSONFromTheProjectFolder(t *testing.T) {
	configSetProject(t, configSetBase) // a react project, node 22, database none, size small
	doc, raw, exit := runConfigOptions(t, "--json")
	if exit != -1 || doc["v"] != float64(1) || doc["project"] != "shop" || doc["type"] != "react" {
		t.Fatalf("exit %d: %s", exit, raw)
	}
	size := doc["size"].(map[string]any)
	if size["key"] != "pod.size" || size["current"] != "small" || len(size["values"].([]any)) != 6 {
		t.Errorf("size %v", size)
	}
	rt := doc["runtime"].(map[string]any)
	if rt["key"] != "runtime.node" || rt["name"] != "node" || rt["current"] != "22" {
		t.Errorf("runtime %v", rt)
	}
	if bd := doc["buildDir"].(map[string]any); bd["applies"] != true || bd["current"] != "dist" {
		t.Errorf("buildDir %v", bd)
	}
}

func TestJSONSnapshotConfigOptions(t *testing.T) {
	m, err := manifest.ParseBytes([]byte(configSetBase))
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshot(t, "config-options", func() { emitConfigOptionsJSON(manifest.OptionsFor(m)) })
}

func TestConfigOptionsByProjectName(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "api")
	doc, raw, exit := runConfigOptions(t, "api", "--json")
	if exit != -1 || doc["project"] != "api" || doc["type"] != "nodejs" {
		t.Fatalf("exit %d: %s", exit, raw)
	}
	if bd := doc["buildDir"].(map[string]any); bd["applies"] != false {
		t.Errorf("buildDir %v", bd)
	}
	doc, raw, exit = runConfigOptions(t, "nope", "--json")
	if exit != 1 || doc["error"].(map[string]any)["code"] != "project-not-found" {
		t.Errorf("unknown project: exit %d %s", exit, raw)
	}
	doc, raw, exit = runConfigOptions(t, "api", "extra", "--json")
	if exit != 1 || doc["error"].(map[string]any)["code"] != "invalid-arguments" {
		t.Errorf("two args: exit %d %s", exit, raw)
	}
}

func TestConfigOptionsHumanLines(t *testing.T) {
	configSetProject(t, configSetBase)
	_, out, exit := runConfigOptions(t)
	want := "size       small    (nano 512M/1 CPU, small 1G/1 CPU, medium 2G/2 CPU, large 4G/4 CPU, xlarge 8G/6 CPU, xxl 16G/8 CPU)\n" +
		"database   none     (none, postgres, mariadb)\n" +
		"node       22       (20, 22, 24)\n" +
		"build-dir  dist\n"
	if exit != -1 || out != want {
		t.Errorf("exit %d\n got %q\nwant %q", exit, out, want)
	}
}
