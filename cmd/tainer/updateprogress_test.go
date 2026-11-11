package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/release"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// tain-125: the self-update download is never silent.

func installWithProgress(t *testing.T, mode tui.OutputMode, tty bool, menu bool) *fakeInstall {
	t.Helper()
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.size = 96468992
	ops := f.ops("")
	ops.yes = true
	ops.progress = newDownloadProgress(mode, &f.out, tty)
	install := installTainer
	if menu {
		install = installMenu
	}
	if _, err := install(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSelfUpdatePlainPrintsDownloadingThenVerifying(t *testing.T) {
	f := installWithProgress(t, tui.ModePlain, false, false)
	if f.outAtOpen != "downloading tainer 1.5.0…\nverifying…\nopening the installer…\n" {
		t.Errorf("printed before the installer opened: %q", f.outAtOpen)
	}
}

func TestSelfUpdateStyledOnATerminalShowsTheBar(t *testing.T) {
	f := installWithProgress(t, tui.ModeStyled, true, false)
	got := f.outAtOpen
	order := []string{
		"downloading tainer 1.5.0",
		"   0%  0 B / 92.0 MB",
		"  50%  46.0 MB / 92.0 MB",
		" 100%  92.0 MB / 92.0 MB",
		tui.MarkSuccess() + "downloaded tainer 1.5.0 (92.0 MB)\n",
		"verifying…",
		tui.MarkSuccess() + "verified\n",
		tui.MarkInfo() + "opening the installer…\n",
	}
	at := 0
	for _, want := range order {
		i := strings.Index(got[at:], want)
		if i < 0 {
			t.Fatalf("missing %q after offset %d in:\n%q", want, at, got)
		}
		at += i + len(want)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("a live line was left open when the installer started: %q", got)
	}
}

func TestSelfUpdateStyledButPipedFallsBackToLines(t *testing.T) {
	f := installWithProgress(t, tui.ModeStyled, false, false)
	if f.outAtOpen != "downloading tainer 1.5.0…\nverifying…\nopening the installer…\n" {
		t.Errorf("got %q", f.outAtOpen)
	}
}

func TestSelfUpdateJSONPrintsNoProgress(t *testing.T) {
	f := installWithProgress(t, tui.ModeJSON, true, false)
	if f.outAtOpen != "" {
		t.Errorf("JSON mode printed %q", f.outAtOpen)
	}
}

func TestSelfUpdateProgressNamesTheMenuApp(t *testing.T) {
	f := installWithProgress(t, tui.ModePlain, false, true)
	if f.outAtOpen != "downloading the menu app 1.5.0…\nverifying…\nopening the installer…\n" {
		t.Errorf("got %q", f.outAtOpen)
	}
}

func TestSelfUpdateFailedVerifyLeavesNoLiveLine(t *testing.T) {
	onMac(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFakeInstall()
	f.size = 100
	f.verifyErr = errors.New("not signed by Cyber5")
	ops := f.ops("")
	ops.yes = true
	ops.progress = newDownloadProgress(tui.ModeStyled, &f.out, true)
	if _, err := installTainer(context.Background(), ops); err == nil {
		t.Fatal("want the verify error")
	}
	out := f.out.String()
	if !strings.HasSuffix(out, "\r\x1b[2K") || strings.Contains(out, "verified") {
		t.Errorf("got %q", out)
	}
}

func TestSelfUpdateSaysOpeningOnlyOnceTheResumeFileIsWritten(t *testing.T) {
	onMac(t)
	// The settings folder can't be created: a file sits where it should be.
	xdg := t.TempDir()
	if err := os.WriteFile(filepath.Join(xdg, "tainer"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	for _, mode := range []tui.OutputMode{tui.ModePlain, tui.ModeStyled} {
		f := newFakeInstall()
		f.size = 100
		f.pods = []release.ResumeEntry{{Name: "shop", Path: "/p/shop"}}
		ops := f.ops("")
		ops.yes = true
		ops.progress = newDownloadProgress(mode, &f.out, true)
		if _, err := installTainer(context.Background(), ops); err == nil {
			t.Fatalf("%v: want the resume-file error", mode)
		}
		out := f.out.String()
		if !strings.Contains(out, "verif") || strings.Contains(out, "opening the installer") {
			t.Errorf("%v: %q", mode, out)
		}
		for _, step := range f.steps {
			if step == "open" {
				t.Errorf("%v: the installer opened", mode)
			}
		}
	}
}
