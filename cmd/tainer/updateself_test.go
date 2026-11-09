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
	"time"

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
	running         []string
	handOffArgs     [][]string
	checkDeadline   []time.Duration
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
	origRun, origHand := runningPodNames, handOffUpdate
	t.Cleanup(func() {
		releaseCheck, installedTainerFor, installedMenuFor, podCheck = origCheck, origT, origM, origPods
		selfInstallTainer, selfInstallMenu, updatePod, updateStdin = origIT, origIM, origPod, origIn
		runningPodNames, handOffUpdate = origRun, origHand
	})
	runningPodNames = func(context.Context) []string { return f.running }
	// The new binary finishes the run: here, this same code with tainer
	// now at the new version.
	handOffUpdate = func(outArgs []string) int {
		f.log = append(f.log, "handoff")
		f.handOffArgs = append(f.handOffArgs, outArgs)
		cmdUpdate(append([]string{"--yes"}, outArgs...))
		return 0
	}
	releaseCheck = func(_ context.Context, force bool) (release.Latest, bool) {
		f.forced = append(f.forced, force)
		return f.latest, f.ok
	}
	installedTainerFor = func() string { return f.tainer }
	installedMenuFor = func() string { return f.menu }
	podCheck = func(ctx context.Context, autoStart bool, projects map[string]registry.Project) (imagecheck.Report, error) {
		if d, ok := ctx.Deadline(); ok {
			f.checkDeadline = append(f.checkDeadline, time.Until(d))
		} else {
			f.checkDeadline = append(f.checkDeadline, -1)
		}
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
		for _, p := range rep.Pods {
			if p.UpdateAvailable {
				rep.Available++
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
	if exit != -1 && exit != 0 {
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
	if got := podLog(f.log); !reflect.DeepEqual(got, []string{"tainer", "handoff", "menu", "pod:blog"}) {
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
	if len(f.log) != 4 {
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
	if !reflect.DeepEqual(f.log, []string{"tainer", "handoff", "menu"}) {
		t.Errorf("ran %v, want tainer before menu", f.log)
	}

	// The full pkg already brought a new enough menu: no separate update.
	f2 := newFakeSelf()
	f2.latest.Menu.MinTainer = "1.5.0"
	f2.menuAfterTainer = "1.5.0"
	f2.install(t)
	runUpdateCmd(t, []string{"--yes"})
	if !reflect.DeepEqual(f2.log, []string{"tainer", "handoff"}) {
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

func TestUpdateNoArgsAsksOnce(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "blog")
	f := newFakeSelf()
	f.running = []string{"blog", "shop"}
	f.stdin = "y\n"
	f.install(t)
	out := string(runUpdateCmd(t, []string{"--plain"}))
	if !strings.Contains(out, "Updating tainer restarts your running pods (blog, shop).\n") {
		t.Errorf("missing the restart warning in the listing:\n%s", out)
	}
	if n := strings.Count(out, "[y/N]"); n != 1 {
		t.Errorf("asked %d times, want once:\n%s", n, out)
	}
	if got := podLog(f.log); !reflect.DeepEqual(got, []string{"tainer", "handoff", "menu", "pod:blog"}) {
		t.Fatalf("ran %v", got)
	}
	if !reflect.DeepEqual(f.opsYes, []bool{true, true}) {
		t.Errorf("installers must not ask again, yes = %v", f.opsYes)
	}
	if !reflect.DeepEqual(f.handOffArgs, [][]string{{"--plain"}}) {
		t.Errorf("hand-off args = %v, want the output flags", f.handOffArgs)
	}
}

func TestMissingMenuAppIsNotAnUpdate(t *testing.T) {
	selfTestHome(t)
	f := newFakeSelf()
	f.menu = ""
	f.latest.Tainer.Version = "1.4.0"
	f.install(t)
	out, _ := failureHarness(t, true, func() { cmdUpdate([]string{"--json", "--check"}) })
	mn, _ := checkDocFrom(t, out)["menu"].(map[string]any)
	if mn["updateAvailable"] != false || mn["installed"] != "" {
		t.Errorf("menu = %v", mn)
	}
	text := string(runUpdateCmd(t, nil))
	if strings.Contains(text, "menu app") || !strings.Contains(text, "everything is up to date") {
		t.Errorf("a missing menu app must not be listed:\n%s", text)
	}
	runUpdateCmd(t, []string{"menu"})
	if !reflect.DeepEqual(f.log, []string{"menu"}) {
		t.Errorf("update menu must still install it, ran %v", f.log)
	}
}

func TestHeldPodsAreListed(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "control")
	f := newFakeSelf()
	f.latest.Tainer.Version = "1.4.0"
	f.latest.Menu.Version = "1.4.0"
	f.report = imagecheck.Report{Pods: []imagecheck.PodReport{{Name: "control", State: "running", Roles: []imagecheck.RoleReport{},
		Control: &imagecheck.ControlCheck{Running: "1.0.0", Stable: "1.2.0", MinTainer: "1.6.0", State: imagecheck.ControlNeedsNewerTainer}}}}
	f.install(t)
	out := string(runUpdateCmd(t, nil))
	if !strings.Contains(out, "control  needs a newer tainer — dashboard 1.2.0 needs tainer 1.6.0 or later") {
		t.Errorf("held pod not listed:\n%s", out)
	}
	if strings.Contains(out, "everything is up to date") || strings.Contains(out, "[y/N]") {
		t.Errorf("a held pod is information, not up to date and not an action:\n%s", out)
	}
	if len(f.log) != 0 {
		t.Errorf("ran %v", f.log)
	}
}

func TestReservedTargetWithClashStillSelfUpdatesForJSON(t *testing.T) {
	cwd := selfTestHome(t)
	registerProject(t, cwd, "menu")
	f := newFakeSelf()
	f.install(t)
	out, exit := failureHarness(t, true, func() { cmdUpdate([]string{"menu", "--json", "--yes"}) })
	if exit != -1 {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
	if !reflect.DeepEqual(f.log, []string{"menu"}) {
		t.Fatalf("update menu --json ran %v, want the menu app", f.log)
	}
	doc := checkDocFrom(t, out)
	if doc["mode"] != "menu" || doc["updated"] != true {
		t.Errorf("doc = %v", doc)
	}
}

func TestHandOffRunsTheInstalledBinary(t *testing.T) {
	cwd := selfTestHome(t)
	f := newFakeSelf()
	f.stdin = "y\n"
	f.install(t)
	handOffUpdate = liveHandOffUpdate
	rec := filepath.Join(t.TempDir(), "rec")
	bin := filepath.Join(t.TempDir(), "tainer")
	script := "#!/bin/sh\necho \"$@\" > " + rec + "\npwd >> " + rec + "\nexit 3\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := installedTainerBin
	installedTainerBin = bin
	t.Cleanup(func() { installedTainerBin = orig })

	_, exit := failureHarness(t, false, func() { cmdUpdate([]string{"--plain"}) })
	if exit != 3 {
		t.Fatalf("exit = %d, want the new binary's 3", exit)
	}
	got, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	want, _ := filepath.EvalSymlinks(cwd)
	gotDir, _ := filepath.EvalSymlinks(lines[len(lines)-1])
	if lines[0] != "update --yes --plain" || gotDir != want {
		t.Errorf("binary saw %q", got)
	}
	if !reflect.DeepEqual(f.log, []string{"tainer"}) {
		t.Errorf("the old process must stop after tainer, ran %v", f.log)
	}
}

func TestNoArgsCheckPhaseIsBounded(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "blog")
	f := newFakeSelf()
	f.stdin = "n\n"
	f.install(t)
	runUpdateCmd(t, nil)
	if len(f.checkDeadline) != 1 || f.checkDeadline[0] <= 0 || f.checkDeadline[0] > 2*time.Minute {
		t.Errorf("check deadline = %v, want within 2m", f.checkDeadline)
	}
}

func TestUpdateTainerJSONKeepsResumeOutputOffStdout(t *testing.T) {
	selfTestHome(t)
	writeResumeFixture(t, release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: resumeT0, Pods: twoPods()})
	fr := &fakeResume{installed: "1.5.0", now: resumeT0.Add(time.Minute)}
	origOps := resumeOpsLive
	t.Cleanup(func() { resumeOpsLive = origOps })
	resumeOpsLive = func() resumeOps {
		o := fr.ops()
		o.out = os.Stdout // as the live wiring does
		return o
	}
	f := newFakeSelf()
	f.install(t)
	selfInstallTainer = func(ctx context.Context, ops installOps) (installResult, error) {
		res := newInstallResult("tainer", "1.4.0", "1.5.0")
		outcome, err := ops.resume(ctx)
		res.Resumed, res.Updated = outcome.Resumed, true
		return res, err
	}
	out, exit := failureHarness(t, true, func() { cmdUpdate([]string{"tainer", "--json", "--yes"}) })
	if exit != -1 {
		t.Fatalf("exit=%d\n%s", exit, out)
	}
	if !reflect.DeepEqual(fr.started, []string{"alpha", "beta"}) {
		t.Fatalf("resume didn't run: %v", fr.started)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout isn't a JSON document: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("more than one document on stdout:\n%s", out)
	}
	if strings.Contains(string(out), "is back") {
		t.Fatalf("resume progress on stdout:\n%s", out)
	}
}

func TestUpdateNoArgsStopsWhenTainerInstallCancelled(t *testing.T) {
	selfTestHome(t)
	registerProject(t, t.TempDir(), "blog")
	f := newFakeSelf()
	f.stdin = "y\n"
	f.install(t)
	selfInstallTainer = func(_ context.Context, ops installOps) (installResult, error) {
		f.log = append(f.log, "tainer")
		// Installer closed without installing.
		return newInstallResult("tainer", "1.4.0", "1.5.0"), nil
	}
	out := string(runUpdateCmd(t, []string{"--plain"}))
	if !reflect.DeepEqual(f.log, []string{"tainer"}) {
		t.Fatalf("a cancelled tainer install must stop the run, ran %v", f.log)
	}
	if !strings.Contains(out, "update cancelled\n") {
		t.Errorf("missing the cancelled line:\n%s", out)
	}
}
