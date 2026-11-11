package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/release"
)

// fakeInstall records the order of every step and what each one saw.
type fakeInstall struct {
	latest       release.Latest
	checkOK      bool
	installed    string
	tainerVer    string
	afterOpen    string // installed version once the installer closes; "" keeps installed
	pods         []release.ResumeEntry
	verifyErr    error
	openErr      error
	outcome      resumeOutcome
	steps        []string
	dir          string
	pkg          string
	resumeAtOpen release.ResumeFile
	resumeSeen   bool
	out          bytes.Buffer
	// size, when set, makes the download report progress at 0, half and all of it.
	size int64
	// outAtOpen is what had been printed when the installer opened.
	outAtOpen string
}

var installT0 = time.Date(2026, 11, 9, 15, 4, 5, 0, time.UTC)

func newFakeInstall() *fakeInstall {
	f := &fakeInstall{checkOK: true, installed: "1.4.0", tainerVer: "1.4.0"}
	f.latest.Tainer = release.Artifact{Version: "1.5.0", URL: "https://get.tainer.dev/tainer-1.5.0.pkg"}
	f.latest.Menu = release.Artifact{Version: "1.5.0", URL: "https://get.tainer.dev/menu-1.5.0.pkg", MinTainer: "1.4.0"}
	return f
}

func (f *fakeInstall) ops(stdin string) installOps {
	return installOps{
		check: func(context.Context) (release.Latest, bool) {
			f.steps = append(f.steps, "check")
			return f.latest, f.checkOK
		},
		download: func(_ context.Context, a release.Artifact, dir string, report func(done, total int64)) (string, error) {
			f.steps = append(f.steps, "download")
			if f.size > 0 && report != nil {
				for _, done := range []int64{0, f.size / 2, f.size} {
					report(done, f.size)
				}
			}
			f.dir = dir
			f.pkg = filepath.Join(dir, "tainer-update.pkg")
			return f.pkg, os.WriteFile(f.pkg, []byte("pkg"), 0o600)
		},
		quarantine: func(_ context.Context, path string) error {
			f.steps = append(f.steps, "quarantine")
			return nil
		},
		verify: func(_ context.Context, path string) error {
			f.steps = append(f.steps, "verify")
			return f.verifyErr
		},
		openAndWait: func(_ context.Context, path string) error {
			f.steps = append(f.steps, "open")
			f.outAtOpen = f.out.String()
			f.resumeAtOpen, f.resumeSeen, _ = release.ReadResume()
			if f.afterOpen != "" {
				f.installed = f.afterOpen
			}
			return f.openErr
		},
		installed:       func() string { return f.installed },
		installedTainer: func() string { return f.tainerVer },
		snapshot: func(context.Context) ([]release.ResumeEntry, error) {
			f.steps = append(f.steps, "snapshot")
			return f.pods, nil
		},
		resume: func(context.Context) (resumeOutcome, error) {
			f.steps = append(f.steps, "resume")
			return f.outcome, nil
		},
		now: func() time.Time { return installT0 },
		in:  bufio.NewReader(strings.NewReader(stdin)),
		out: &f.out,
	}
}

func onMac(t *testing.T) {
	t.Helper()
	old := selfUpdateSupported
	selfUpdateSupported = true
	t.Cleanup(func() { selfUpdateSupported = old })
}

func TestInstallTainerWarnsAndPostpones(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.pods = []release.ResumeEntry{{Name: "shop", Path: "/p/shop"}, {Name: "blog", Path: "/p/blog"}, {Name: "c5test", Path: "/p/c5test"}}
	res, err := installTainer(context.Background(), f.ops("n\n"))
	if err != nil {
		t.Fatal(err)
	}
	out := f.out.String()
	if !strings.Contains(out, "Updating tainer restarts your running pods (shop, blog, c5test).\nUpdate now? [y/N] ") {
		t.Fatalf("missing warning, got %q", out)
	}
	if !strings.Contains(out, "update postponed") {
		t.Fatalf("missing postponed line, got %q", out)
	}
	if want := []string{"check", "snapshot"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("steps %v, want %v", f.steps, want)
	}
	if resumeExists(t) {
		t.Fatal("resume file written for a postponed update")
	}
	if res.Updated || res.Mode != "tainer" || res.From != "1.4.0" || res.To != "1.5.0" {
		t.Fatalf("result %+v", res)
	}
}

func TestInstallTainerNoRunningPodsNoWarning(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.afterOpen = "1.5.0"
	// No stdin answer: a prompt would read EOF and postpone.
	res, err := installTainer(context.Background(), f.ops(""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.out.String(), "restarts your running pods") || strings.Contains(f.out.String(), "[y/N]") {
		t.Fatalf("unexpected warning: %q", f.out.String())
	}
	if want := []string{"check", "snapshot", "download", "quarantine", "verify", "open", "resume"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("steps %v, want %v", f.steps, want)
	}
	if !res.Updated {
		t.Fatalf("result %+v, want updated", res)
	}
}

func TestInstallTainerYesSkipsPrompt(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.pods = []release.ResumeEntry{{Name: "shop", Path: "/p/shop"}}
	ops := f.ops("")
	ops.yes = true
	if _, err := installTainer(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.out.String(), "[y/N]") {
		t.Fatalf("prompted despite --yes: %q", f.out.String())
	}
	if !strings.Contains(f.out.String(), "Updating tainer restarts your running pods (shop).\n") {
		t.Fatalf("warning missing with --yes: %q", f.out.String())
	}
	if !strings.Contains(strings.Join(f.steps, ","), "open") {
		t.Fatalf("installer not opened: %v", f.steps)
	}
}

func TestInstallTainerVerifyFailureWritesNothing(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.pods = []release.ResumeEntry{{Name: "shop", Path: "/p/shop"}}
	f.verifyErr = errors.New("the download isn't signed by Cyber5")
	_, err := installTainer(context.Background(), f.ops("y\n"))
	if err == nil || !strings.Contains(err.Error(), "isn't signed by Cyber5") {
		t.Fatalf("err = %v", err)
	}
	if resumeExists(t) {
		t.Fatal("resume file written after a failed verify")
	}
	for _, s := range f.steps {
		if s == "open" || s == "resume" {
			t.Fatalf("steps %v went past verify", f.steps)
		}
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Fatalf("temp dir %s left behind (stat err %v)", f.dir, err)
	}
}

func TestInstallTainerWritesResumeBeforeOpening(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.afterOpen = "1.5.0"
	f.pods = []release.ResumeEntry{{Name: "blog", Path: "/p/blog"}}
	f.outcome = resumeOutcome{Resumed: []string{"blog"}, Failed: []resumeFailure{}}
	res, err := installTainer(context.Background(), f.ops("yes\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"check", "snapshot", "download", "quarantine", "verify", "open", "resume"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("steps %v, want %v", f.steps, want)
	}
	if !f.resumeSeen {
		t.Fatal("no resume file when the installer opened")
	}
	want := release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: installT0, Pods: f.pods}
	got := f.resumeAtOpen
	got.WrittenAt = got.WrittenAt.UTC()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resume file %+v, want %+v", got, want)
	}
	if !res.Updated || !reflect.DeepEqual(res.Resumed, []string{"blog"}) || res.From != "1.4.0" || res.To != "1.5.0" {
		t.Fatalf("result %+v", res)
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Fatalf("temp dir %s left behind", f.dir)
	}
}

func TestInstallTainerTempDirIsPrivate(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	ops := f.ops("")
	var mode os.FileMode
	dl := ops.download
	ops.download = func(ctx context.Context, a release.Artifact, dir string, report func(done, total int64)) (string, error) {
		st, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		mode = st.Mode().Perm()
		return dl(ctx, a, dir, report)
	}
	if _, err := installTainer(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	if mode != 0o700 {
		t.Fatalf("temp dir mode %o, want 700", mode)
	}
}

func TestInstallTainerCancelledInstallNotUpdated(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.pods = []release.ResumeEntry{{Name: "blog", Path: "/p/blog"}}
	res, err := installTainer(context.Background(), f.ops("y\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated {
		t.Fatalf("result %+v, want not updated", res)
	}
	if f.steps[len(f.steps)-1] != "resume" {
		t.Fatalf("resume not run after a cancelled install: %v", f.steps)
	}
}

func TestInstallTainerOpenFailureStillResumes(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.openErr = errors.New("boom")
	if _, err := installTainer(context.Background(), f.ops("")); err == nil {
		t.Fatal("want an error when the installer can't open")
	}
	if f.steps[len(f.steps)-1] != "resume" {
		t.Fatalf("resume not run: %v", f.steps)
	}
}

func TestInstallTainerUpToDate(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.installed = "1.5.0"
	res, err := installTainer(context.Background(), f.ops(""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "tainer is up to date") {
		t.Fatalf("output %q", f.out.String())
	}
	if want := []string{"check"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("steps %v, want %v", f.steps, want)
	}
	if res.Updated || res.From != "1.5.0" || res.To != "1.5.0" {
		t.Fatalf("result %+v", res)
	}
}

func TestInstallTainerCheckFailure(t *testing.T) {
	onMac(t)
	f := newFakeInstall()
	f.checkOK = false
	if _, err := installTainer(context.Background(), f.ops("")); err == nil {
		t.Fatal("want an error when there's no release info")
	}
	if want := []string{"check"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("steps %v, want %v", f.steps, want)
	}
}

func TestInstallMenuNeedsNewerTainer(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.latest.Menu.MinTainer = "1.6.0"
	_, err := installMenu(context.Background(), f.ops(""))
	if err == nil || err.Error() != "the menu app needs tainer 1.6.0 first" {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"check"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("steps %v, want %v", f.steps, want)
	}
}

func TestInstallMenuNoWarningNoResume(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.afterOpen = "1.5.0"
	f.pods = []release.ResumeEntry{{Name: "shop", Path: "/p/shop"}}
	res, err := installMenu(context.Background(), f.ops(""))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"check", "download", "quarantine", "verify", "open"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("steps %v, want %v", f.steps, want)
	}
	if f.resumeSeen || resumeExists(t) {
		t.Fatal("menu update wrote a resume file")
	}
	if !res.Updated || res.Mode != "menu" || res.From != "1.4.0" || res.To != "1.5.0" {
		t.Fatalf("result %+v", res)
	}
}

func TestInstallMenuUpToDate(t *testing.T) {
	onMac(t)
	f := newFakeInstall()
	f.installed = "1.5.0"
	if _, err := installMenu(context.Background(), f.ops("")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "the menu app is up to date") {
		t.Fatalf("output %q", f.out.String())
	}
}

func TestInstallRefusedOffMac(t *testing.T) {
	old := selfUpdateSupported
	selfUpdateSupported = false
	t.Cleanup(func() { selfUpdateSupported = old })
	f := newFakeInstall()
	const want = "updating tainer itself works on macOS only — download the new version from https://tainer.dev"
	if _, err := installTainer(context.Background(), f.ops("")); err == nil || err.Error() != want {
		t.Fatalf("tainer err = %v", err)
	}
	if _, err := installMenu(context.Background(), f.ops("")); err == nil || err.Error() != want {
		t.Fatalf("menu err = %v", err)
	}
	if len(f.steps) != 0 {
		t.Fatalf("steps %v, want none", f.steps)
	}
}

func TestInstallResultJSONShape(t *testing.T) {
	r := newInstallResult("tainer", "1.4.0", "1.5.0")
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"mode":"tainer","from":"1.4.0","to":"1.5.0","updated":false,"resumed":[],"failed":[]}`
	if string(b) != want {
		t.Fatalf("json %s, want %s", b, want)
	}
}

func TestRunResumeReportOutcome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pods := []release.ResumeEntry{{Name: "alpha", Path: "/p/alpha"}, {Name: "beta", Path: "/p/beta"}}
	writeResumeFixture(t, release.ResumeFile{ForVersion: "1.5.0", FromVersion: "1.4.0", WrittenAt: resumeT0, Pods: pods})
	f := &fakeResume{installed: "1.5.0", now: resumeT0, fail: map[string]error{"beta": fmt.Errorf("reading manifest: %w", os.ErrNotExist)}}
	got, err := runResumeReport(context.Background(), f.ops(), true)
	if err != nil {
		t.Fatal(err)
	}
	want := resumeOutcome{Resumed: []string{"alpha"}, Failed: []resumeFailure{{Name: "beta", Reason: "its folder or tainer.yaml is gone"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outcome %+v, want %+v", got, want)
	}
}

func TestConfirmChainsOnOneReader(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("y\nn\ny\n"))
	var out bytes.Buffer
	got := []bool{confirm(in, &out, "a?"), confirm(in, &out, "b?"), confirm(in, &out, "c?")}
	if want := []bool{true, false, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("answers %v, want %v", got, want)
	}
}

func TestInstallMenuInstallsWhenMissing(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.installed = ""
	f.afterOpen = "1.5.0"
	res, err := installMenu(context.Background(), f.ops(""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.out.String(), "up to date") {
		t.Fatalf("missing menu app reported up to date: %q", f.out.String())
	}
	if want := []string{"check", "download", "quarantine", "verify", "open"}; !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("steps %v, want %v", f.steps, want)
	}
	if !res.Updated || res.From != "" || res.To != "1.5.0" {
		t.Fatalf("result %+v", res)
	}
}

func TestInstallMenuMissingStillNeedsTainer(t *testing.T) {
	onMac(t)
	f := newFakeInstall()
	f.installed = ""
	f.latest.Menu.MinTainer = "1.6.0"
	if _, err := installMenu(context.Background(), f.ops("")); err == nil || err.Error() != "the menu app needs tainer 1.6.0 first" {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallTainerInterruptedLeavesResumeFile(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.pods = []release.ResumeEntry{{Name: "blog", Path: "/p/blog"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ops := f.ops("y\n")
	open := ops.openAndWait
	ops.openAndWait = func(c context.Context, path string) error {
		_ = open(c, path)
		cancel() // Ctrl-C while Installer is open
		return c.Err()
	}
	_, err := installTainer(ctx, ops)
	if err == nil || err.Error() != "the update was interrupted; your pods come back when the install finishes" {
		t.Fatalf("err = %v", err)
	}
	if f.steps[len(f.steps)-1] == "resume" {
		t.Fatalf("resumed after an interrupt: %v", f.steps)
	}
	if !resumeExists(t) {
		t.Fatal("resume file dropped after an interrupt")
	}
}
