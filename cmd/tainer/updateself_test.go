package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/release"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// fakeSelf stands in for the release endpoint, the installed versions, the
// pod check and every installer, and records what ran in order.
type fakeSelf struct {
	latest  release.Latest
	ok      bool
	tainer  string
	menu    string
	report  imagecheck.Report
	podErr  error
	stdin   string
	forced  []bool
	checked [][]string
	auto    []bool
	log     []string
	// menuAfterTainer is the menu version the full pkg leaves behind.
	menuAfterTainer string
	opsYes          []bool
}

func newFakeSelf() *fakeSelf {
	f := &fakeSelf{ok: true, tainer: "1.4.0", menu: "1.4.0"}
	f.latest.Tainer = release.Artifact{Version: "1.5.0", Notes: "Faster starts"}
	f.latest.Menu = release.Artifact{Version: "1.5.0", Notes: "New badge", MinTainer: "1.4.0"}
	f.report = imagecheck.Report{CheckedAt: "2026-11-09T12:00:00.000Z", Pods: []imagecheck.PodReport{
		{Name: "blog", State: "running", UpdateAvailable: true, Parts: "WordPress · PHP 8.3", Roles: []imagecheck.RoleReport{}},
		{Name: "shop", State: "stopped", Parts: "React · Node 22", Roles: []imagecheck.RoleReport{}},
	}, Available: 1, UpToDate: 1}
	return f
}

func (f *fakeSelf) install(t *testing.T) {
	t.Helper()
	origCheck, origT, origM, origPods := releaseCheck, installedTainerFor, installedMenuFor, podCheck
	origIT, origIM, origPod, origIn := selfInstallTainer, selfInstallMenu, updatePod, updateStdin
	t.Cleanup(func() {
		releaseCheck, installedTainerFor, installedMenuFor, podCheck = origCheck, origT, origM, origPods
		selfInstallTainer, selfInstallMenu, updatePod, updateStdin = origIT, origIM, origPod, origIn
	})
	releaseCheck = func(_ context.Context, force bool) (release.Latest, bool) {
		f.forced = append(f.forced, force)
		return f.latest, f.ok
	}
	installedTainerFor = func() string { return f.tainer }
	installedMenuFor = func() string { return f.menu }
	podCheck = func(_ context.Context, autoStart bool, projects map[string]registry.Project) (imagecheck.Report, error) {
		names := make([]string, 0, len(projects))
		for n := range projects {
			names = append(names, n)
		}
		sort.Strings(names)
		f.checked = append(f.checked, names)
		f.auto = append(f.auto, autoStart)
		if f.podErr != nil {
			return imagecheck.Report{}, f.podErr
		}
		var rep imagecheck.Report
		rep.CheckedAt = f.report.CheckedAt
		for _, p := range f.report.Pods {
			if _, ok := projects[p.Name]; ok {
				rep.Pods = append(rep.Pods, p)
			}
		}
		return rep, nil
	}
	selfInstallTainer = func(_ context.Context, ops installOps) (installResult, error) {
		f.log = append(f.log, "tainer")
		f.opsYes = append(f.opsYes, ops.yes)
		if f.menuAfterTainer != "" {
			f.menu = f.menuAfterTainer
		}
		f.tainer = f.latest.Tainer.Version
		return installResult{Mode: "tainer", From: "1.4.0", To: f.latest.Tainer.Version, Updated: true, Resumed: []string{}, Failed: []resumeFailure{}}, nil
	}
	selfInstallMenu = func(_ context.Context, ops installOps) (installResult, error) {
		f.log = append(f.log, "menu")
		f.opsYes = append(f.opsYes, ops.yes)
		return installResult{Mode: "menu", From: f.menu, To: f.latest.Menu.Version, Updated: true, Resumed: []string{}, Failed: []resumeFailure{}}, nil
	}
	updatePod = func(_ context.Context, name, manifestPath, dir string) (updateOutcome, error) {
		f.log = append(f.log, "pod:"+name+"@"+dir)
		return updateOutcome{Started: true}, nil
	}
	updateStdin = strings.NewReader(f.stdin)
}

// selfTestHome gives the test its own registry and a cwd outside any project.
func selfTestHome(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cwd := t.TempDir()
	t.Chdir(cwd)
	return cwd
}

// registerProject writes a project folder with a manifest and registers it.
func registerProject(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: 2\nproject:\n  name: " + name + "\n  type: nodejs\n  domain: " + name + ".tainer.me\nruntime:\n  node: 22\n  database: none\n"
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(name, dir, "nodejs", name+".tainer.me"); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runUpdateCmd runs cmdUpdate and fails the test if it exits.
func runUpdateCmd(t *testing.T, args []string) []byte {
	t.Helper()
	out, exit := failureHarness(t, false, func() { cmdUpdate(args) })
	if exit != -1 {
		t.Fatalf("cmdUpdate(%v) exited %d\n%s", args, exit, out)
	}
	return out
}

func podLog(log []string) []string {
	out := []string{}
	for _, l := range log {
		if i := strings.Index(l, "@"); i > 0 {
			l = l[:i]
		}
		out = append(out, l)
	}
	return out
}

func checkDocFrom(t *testing.T, out []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return doc
}

func TestCheckAddsTainerAndMenuKeys(t *testing.T) {
	// The golden path is relative: stay in the package folder.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeSelf()
	f.latest.Tainer.Notes = "Faster starts"
	f.latest.Menu = release.Artifact{Version: "1.5.0", Notes: "New badge", MinTainer: "1.5.0"}
	f.report = goldenCheckReport()
	f.install(t)
	podCheck = func(context.Context, bool, map[string]registry.Project) (imagecheck.Report, error) {
		return goldenCheckReport(), nil
	}
	assertSnapshot(t, "update-check", func() { runUpdateCheck(tui.ModeJSON) })
	if !reflect.DeepEqual(f.forced, []bool{true}) {
		t.Errorf("--check must force a fresh release fetch, got %v", f.forced)
	}
}

func TestCheckReportsTainerUpdateWithStackDown(t *testing.T) {
	selfTestHome(t)
	t.Setenv("DOCKER_HOST", "unix://"+t.TempDir()+"/missing.sock")
	f := newFakeSelf()
	f.install(t)
	podCheck = livePodCheck
	out, exit := failureHarness(t, true, func() { cmdUpdate([]string{"--json", "--check"}) })
	if exit != -1 {
		t.Fatalf("exit = %d, want none (0)\n%s", exit, out)
	}
	doc := checkDocFrom(t, out)
	if doc["podsCheck"] != "unavailable" || doc["releaseCheck"] != "ok" {
		t.Errorf("podsCheck/releaseCheck = %v/%v", doc["podsCheck"], doc["releaseCheck"])
	}
	if pods, ok := doc["pods"].([]any); !ok || len(pods) != 0 {
		t.Errorf("pods = %#v, want []", doc["pods"])
	}
	tn, _ := doc["tainer"].(map[string]any)
	if tn["installed"] != "1.4.0" || tn["available"] != "1.5.0" || tn["updateAvailable"] != true || tn["notes"] != "Faster starts" {
		t.Errorf("tainer = %v", tn)
	}
}

func TestCheckListsPodsWhenReleaseFetchFails(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "blog")
	f := newFakeSelf()
	f.ok = false
	f.latest = release.Latest{}
	f.install(t)
	out, exit := failureHarness(t, true, func() { cmdUpdate([]string{"--json", "--check"}) })
	if exit != -1 {
		t.Fatalf("exit = %d\n%s", exit, out)
	}
	doc := checkDocFrom(t, out)
	if doc["releaseCheck"] != "unavailable" || doc["podsCheck"] != "ok" {
		t.Errorf("releaseCheck/podsCheck = %v/%v", doc["releaseCheck"], doc["podsCheck"])
	}
	if pods, _ := doc["pods"].([]any); len(pods) != 1 {
		t.Errorf("pods = %v", doc["pods"])
	}
	tn, _ := doc["tainer"].(map[string]any)
	if tn["updateAvailable"] != false || tn["installed"] != "1.4.0" {
		t.Errorf("tainer = %v", tn)
	}
}

func TestUpdateNoArgsListsAndAsks(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "blog")
	registerProject(t, t.TempDir(), "shop")

	f := newFakeSelf()
	f.stdin = "n\n"
	f.install(t)
	out := runUpdateCmd(t, nil)
	for _, want := range []string{"tainer 1.4.0 → 1.5.0", "menu app 1.4.0 → 1.5.0", "blog", "Update all of these? [y/N]"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "shop") {
		t.Errorf("an up-to-date pod must not be listed:\n%s", out)
	}
	if len(f.log) != 0 {
		t.Fatalf("answering n must update nothing, ran %v", f.log)
	}
	if !reflect.DeepEqual(f.forced, []bool{false}) || !reflect.DeepEqual(f.auto, []bool{false}) {
		t.Errorf("listing: forced=%v autoStart=%v, want cached release and no auto-start", f.forced, f.auto)
	}

	f = newFakeSelf()
	f.install(t)
	out = runUpdateCmd(t, []string{"--yes"})
	if got := podLog(f.log); !reflect.DeepEqual(got, []string{"tainer", "menu", "pod:blog"}) {
		t.Fatalf("order = %v, want tainer, menu, pods\n%s", got, out)
	}
	if strings.Contains(string(out), "[y/N]") {
		t.Errorf("--yes must not ask:\n%s", out)
	}
	if !reflect.DeepEqual(f.opsYes, []bool{true, true}) {
		t.Errorf("installers must get yes, got %v", f.opsYes)
	}

	f = newFakeSelf()
	f.install(t)
	runUpdateCmd(t, []string{"-y"})
	if len(f.log) != 3 {
		t.Errorf("-y must work like --yes, ran %v", f.log)
	}
}

func TestUpdateNoArgsInProjectOffersOnlyThatPod(t *testing.T) {
	selfTestHome(t)
	parent := t.TempDir()
	blog := registerProject(t, parent, "blog")
	registerProject(t, parent, "shop")
	sub := filepath.Join(blog, "html")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	f := newFakeSelf()
	f.stdin = "y\n"
	f.install(t)
	out := runUpdateCmd(t, nil)
	s := string(out)
	if !strings.Contains(s, "Update blog? [y/N]") {
		t.Errorf("must ask about blog:\n%s", s)
	}
	if !strings.Contains(s, `A newer tainer is available — run "tainer update" outside a project folder to see everything.`) {
		t.Errorf("missing the tainer hint:\n%s", s)
	}
	if strings.Contains(s, "menu app") || strings.Contains(s, "1.4.0 → 1.5.0") {
		t.Errorf("tainer/menu must not be offered here:\n%s", s)
	}
	if !reflect.DeepEqual(f.log, []string{"pod:blog@" + blog}) {
		t.Fatalf("ran %v, want only blog", f.log)
	}
	if !reflect.DeepEqual(f.checked, [][]string{{"blog"}}) {
		t.Errorf("checked %v, want only blog", f.checked)
	}

	// No tainer update: no hint.
	f = newFakeSelf()
	f.latest.Tainer.Version = "1.4.0"
	f.stdin = "n\n"
	f.install(t)
	out = runUpdateCmd(t, nil)
	if strings.Contains(string(out), "A newer tainer") {
		t.Errorf("no hint without a tainer update:\n%s", out)
	}
	if len(f.log) != 0 {
		t.Errorf("n must update nothing: %v", f.log)
	}
}

func TestUpdateNothingToDo(t *testing.T) {
	selfTestHome(t)
	parent := t.TempDir()
	shop := registerProject(t, parent, "shop")

	f := newFakeSelf()
	f.latest.Tainer.Version = "1.4.0"
	f.latest.Menu.Version = "1.4.0"
	f.install(t)
	out := runUpdateCmd(t, nil)
	if !strings.Contains(string(out), "everything is up to date") || strings.Contains(string(out), "[y/N]") {
		t.Errorf("outside a project:\n%s", out)
	}

	t.Chdir(shop)
	out = runUpdateCmd(t, nil)
	if !strings.Contains(string(out), "shop is up to date") || strings.Contains(string(out), "[y/N]") {
		t.Errorf("inside a project:\n%s", out)
	}
	if len(f.log) != 0 {
		t.Errorf("nothing should run: %v", f.log)
	}
}

func TestReservedTargetsDontShadowProjects(t *testing.T) {
	cwd := selfTestHome(t)
	dir := registerProject(t, cwd, "menu")

	f := newFakeSelf()
	f.install(t)
	out := runUpdateCmd(t, []string{"menu"})
	if !reflect.DeepEqual(f.log, []string{"menu"}) {
		t.Fatalf("update menu ran %v, want the menu app", f.log)
	}
	if !strings.Contains(string(out), `(to update the project named "menu", run tainer update ./menu)`) {
		t.Errorf("missing hint:\n%s", out)
	}

	f.log = nil
	runUpdateCmd(t, []string{"./menu"})
	if !reflect.DeepEqual(f.log, []string{"pod:menu@" + dir}) {
		t.Fatalf("update ./menu ran %v", f.log)
	}

	f.log = nil
	t.Chdir(dir)
	runUpdateCmd(t, []string{"."})
	if !reflect.DeepEqual(f.log, []string{"pod:menu@" + dir}) {
		t.Fatalf("update . ran %v", f.log)
	}

	// No project named tainer: no hint.
	f.log = nil
	out = runUpdateCmd(t, []string{"tainer"})
	if !reflect.DeepEqual(f.log, []string{"tainer"}) || strings.Contains(string(out), "project named") {
		t.Errorf("update tainer ran %v\n%s", f.log, out)
	}
}

func TestUpdateTargetJSONEmitsInstallResult(t *testing.T) {
	selfTestHome(t)
	f := newFakeSelf()
	f.install(t)
	out, exit := failureHarness(t, true, func() { cmdUpdate([]string{"tainer", "--json", "--yes"}) })
	if exit != -1 {
		t.Fatalf("exit=%d\n%s", exit, out)
	}
	doc := checkDocFrom(t, out)
	if doc["v"] != float64(1) || doc["mode"] != "tainer" || doc["updated"] != true || doc["to"] != "1.5.0" {
		t.Errorf("doc = %v", doc)
	}
}

func TestMenuNeedsTainerFirst(t *testing.T) {
	selfTestHome(t)
	f := newFakeSelf()
	f.latest.Menu.MinTainer = "1.5.0"
	f.install(t)

	out, _ := failureHarness(t, true, func() { cmdUpdate([]string{"--json", "--check"}) })
	doc := checkDocFrom(t, out)
	mn, _ := doc["menu"].(map[string]any)
	if mn["needsTainerFirst"] != true || mn["minTainer"] != "1.5.0" || mn["updateAvailable"] != true {
		t.Errorf("menu = %v", mn)
	}

	f.log = nil
	f.stdin = "n\n"
	updateStdin = strings.NewReader(f.stdin)
	text := runUpdateCmd(t, nil)
	if !strings.Contains(string(text), "needs tainer 1.5.0 first") {
		t.Errorf("listing must say the menu needs tainer first:\n%s", text)
	}

	f.log = nil
	runUpdateCmd(t, []string{"--yes"})
	if !reflect.DeepEqual(f.log, []string{"tainer", "menu"}) {
		t.Errorf("ran %v, want tainer before menu", f.log)
	}

	// The full pkg already brought a new enough menu: no separate update.
	f2 := newFakeSelf()
	f2.latest.Menu.MinTainer = "1.5.0"
	f2.menuAfterTainer = "1.5.0"
	f2.install(t)
	runUpdateCmd(t, []string{"--yes"})
	if !reflect.DeepEqual(f2.log, []string{"tainer"}) {
		t.Errorf("ran %v, want only tainer once the pkg brought the menu", f2.log)
	}
}

func goldenCheckReport() imagecheck.Report {
	return imagecheck.Report{
		CheckedAt: "2026-11-02T12:00:00.000Z",
		Pods: []imagecheck.PodReport{
			{Name: "blog", Type: "wordpress", Domain: "blog.tainer.me", State: "running", UpdateAvailable: true,
				Parts: "WordPress · PHP 8.3 · MariaDB 11 · Caddy",
				Roles: []imagecheck.RoleReport{
					{Role: "web", Image: "ghcr.io/cyber5-io/tainer-caddy-web:2-alpine"},
					{Role: "app", Image: "ghcr.io/cyber5-io/tainer-wordpress-app:php-8.3", UpdateAvailable: true},
				}},
			{Name: "shop", Type: "react", Domain: "shop.tainer.me", State: "stopped",
				Parts: "React · Node 22 · Caddy", Roles: []imagecheck.RoleReport{}, Error: "registry unreachable"},
		},
		Available: 1,
		Unknown:   1,
	}
}
