package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/controldev"
)

type devFake struct {
	tty     bool
	applied []string
	boots   []bool
}

func (f *devFake) ops(stdin string) controlDevOps {
	return controlDevOps{
		stdin:      strings.NewReader(stdin),
		isTerminal: func() bool { return f.tty },
		cwd:        func() (string, error) { return "", os.ErrNotExist },
		apply: func(_ context.Context, dir string, boot bool) error {
			f.applied = append(f.applied, dir)
			f.boots = append(f.boots, boot)
			return nil
		},
	}
}

func devCheckout(t *testing.T, name string, marked bool) string {
	t.Helper()
	dir := t.TempDir()
	body := "version: 1\nproject:\n  name: " + name + "\n  type: nodejs\n  domain: devmy.tainer.me\n"
	if marked {
		body += "  control-dev: true\n"
	}
	body += "runtime:\n  node: \"22\"\n  database: none\n"
	if err := os.WriteFile(filepath.Join(dir, "tainer.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runDev(t *testing.T, f *devFake, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runControlDev(context.Background(), args, &out, &errb, f.ops(stdin))
	return code, out.String(), errb.String()
}

func TestAllowRefusesNonInteractiveStdin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := devCheckout(t, "control-dev", true)
	f := &devFake{tty: false}
	code, _, errs := runDev(t, f, "y\n", "allow", dir)
	if code == 0 {
		t.Fatal("piped stdin must be refused")
	}
	if !strings.Contains(errs, "run this in a terminal") {
		t.Errorf("stderr = %q", errs)
	}
	if controldev.Approved(dir) || len(f.applied) != 0 {
		t.Error("nothing may be recorded from piped stdin")
	}
}

func TestAllowRefusesAFolderWithoutTheMarker(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := devCheckout(t, "control-dev", false)
	code, _, errs := runDev(t, &devFake{tty: true}, "y\n", "allow", dir)
	if code == 0 || controldev.Approved(dir) {
		t.Fatal("a folder without control-dev: true must be refused")
	}
	if !strings.Contains(errs, "control-dev: true") {
		t.Errorf("stderr = %q", errs)
	}
}

func TestAllowRefusesAFolderWithoutAProject(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	if code, _, _ := runDev(t, &devFake{tty: true}, "y\n", "allow", dir); code == 0 || controldev.Approved(dir) {
		t.Fatal("a folder without tainer.yaml must be refused")
	}
}

func TestAllowRefusesTheNameControl(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := devCheckout(t, "control", true)
	code, _, errs := runDev(t, &devFake{tty: true}, "y\n", "allow", dir)
	if code == 0 || controldev.Approved(dir) {
		t.Fatal("the reserved name must be refused")
	}
	if !strings.Contains(errs, "control") {
		t.Errorf("stderr = %q", errs)
	}
}

func TestAllowShowsTheFolderAndWarningAndRecordsOnYes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := devCheckout(t, "control-dev", true)
	f := &devFake{tty: true}
	code, out, errs := runDev(t, f, "y\n", "allow", dir)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errs)
	}
	all := out + errs
	canon, _ := controldev.Canonical(dir)
	for _, want := range []string{canon, "This project will be able to run any tainer command on this machine, including deleting pods and reading their databases.", "Allow? [y/N]"} {
		if !strings.Contains(all, want) {
			t.Errorf("output missing %q:\n%s", want, all)
		}
	}
	if !controldev.Approved(dir) {
		t.Fatal("yes must record the approval")
	}
	if len(f.applied) != 1 || f.applied[0] != canon || f.boots[0] {
		t.Errorf("apply = %v boots=%v, want once for the folder without starting tainer", f.applied, f.boots)
	}
}

func TestAllowDefaultsToNo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := devCheckout(t, "control-dev", true)
	for _, answer := range []string{"\n", "n\n", "", "yes please\n"} {
		f := &devFake{tty: true}
		code, _, _ := runDev(t, f, answer, "allow", dir)
		if code == 0 || controldev.Approved(dir) || len(f.applied) != 0 {
			t.Errorf("answer %q: code=%d approved=%v", answer, code, controldev.Approved(dir))
		}
	}
}

func TestAllowDefaultsToTheCurrentFolder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := devCheckout(t, "control-dev", true)
	f := &devFake{tty: true}
	ops := f.ops("y\n")
	ops.cwd = func() (string, error) { return dir, nil }
	var out, errb bytes.Buffer
	if code := runControlDev(context.Background(), []string{"allow"}, &out, &errb, ops); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if !controldev.Approved(dir) {
		t.Fatal("cwd not approved")
	}
}

func TestRevokeRemovesThePath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := devCheckout(t, "control-dev", true)
	if err := controldev.Allow(dir); err != nil {
		t.Fatal(err)
	}
	f := &devFake{}
	if code, _, errs := runDev(t, f, "", "revoke", dir); code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if controldev.Approved(dir) {
		t.Fatal("still approved")
	}
	if len(f.applied) != 1 || f.boots[0] {
		t.Errorf("revoke must apply at once to a running tainer without starting one: %v %v", f.applied, f.boots)
	}
	if code, _, _ := runDev(t, &devFake{}, "", "revoke", dir); code == 0 {
		t.Error("revoking an unapproved folder must fail")
	}
}

func TestListPrintsApprovedPaths(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		if err := controldev.Allow(d); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ := runDev(t, &devFake{}, "", "list")
	ca, _ := controldev.Canonical(a)
	cb, _ := controldev.Canonical(b)
	if code != 0 || out != ca+"\n"+cb+"\n" {
		t.Errorf("code %d, out %q", code, out)
	}
}

func TestUnknownDevSubcommandIsUsage(t *testing.T) {
	if code, _, errs := runDev(t, &devFake{}, "", "frobnicate"); code != 2 || !strings.Contains(errs, "tainer control dev allow") {
		t.Errorf("code %d, stderr %q", code, errs)
	}
}

func TestControlStaysOutOfMainHelp(t *testing.T) {
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	usage()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if strings.Contains(buf.String(), "control") {
		t.Errorf("main help mentions control:\n%s", buf.String())
	}
}

func TestAllowRefusesOtherProjectTypes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	body := "version: 1\nproject:\n  name: blog\n  type: wordpress\n  domain: blog.tainer.me\n  control-dev: true\nruntime:\n  php: \"8.3\"\n  database: mariadb\n"
	if err := os.WriteFile(filepath.Join(dir, "tainer.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runDev(t, &devFake{tty: true}, "y\n", "allow", dir)
	if code == 0 || controldev.Approved(dir) {
		t.Fatal("a wordpress folder must be refused")
	}
	if !strings.Contains(errs, "nodejs") {
		t.Errorf("stderr = %q", errs)
	}
}

func TestRevokeFailureTellsTheUserToStopThePod(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := devCheckout(t, "control-dev", true)
	if err := controldev.Allow(dir); err != nil {
		t.Fatal(err)
	}
	ops := (&devFake{}).ops("")
	ops.apply = func(context.Context, string, bool) error { return os.ErrDeadlineExceeded }
	var out, errb bytes.Buffer
	if code := runControlDev(context.Background(), []string{"revoke", dir}, &out, &errb, ops); code == 0 {
		t.Fatal("a failed apply must fail the command")
	}
	if !strings.Contains(errb.String(), "tainer stop control-dev") || strings.Contains(errb.String(), "next time") {
		t.Errorf("stderr = %q", errb.String())
	}
	if controldev.Approved(dir) {
		t.Error("the approval is still removed")
	}
}
