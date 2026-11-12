// Package scripts holds tests for the shell scripts shipped in the packages.
package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// cleanLaunchItems sources tainer-uninstall.sh (which then only defines its
// functions) and runs clean_launch_items against a fake root, with a fake
// launchctl that records its arguments.
func cleanLaunchItems(t *testing.T, root, uid string) (calls []string, out string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "launchctl.log")
	fake := filepath.Join(t.TempDir(), "launchctl")
	if err := os.WriteFile(fake, []byte("#!/bin/bash\necho \"$*\" >> "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs("tainer-uninstall.sh")
	cmd := exec.Command("bash", "-c", `source "$1" && clean_launch_items "$2" "$3"`, "test", script, root, uid)
	cmd.Env = append(os.Environ(), "LAUNCHCTL="+fake)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	data, _ := os.ReadFile(log)
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l != "" {
			calls = append(calls, l)
		}
	}
	return calls, string(b)
}

func touch(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("<plist/>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func remaining(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func TestUninstallRemovesOnlyTainersLaunchItems(t *testing.T) {
	root := t.TempDir()
	touch(t, root,
		// ours
		"Library/LaunchAgents/io.cyber5.tainer.menu.plist",
		"Library/LaunchDaemons/io.cyber5.cyberstack.pf.plist",
		"Library/LaunchAgents/tainer-helper.plist",
		"Library/LaunchDaemons/com.github.containers.tainer.helper-lenineto.plist",
		"private/var/run/tainer-helper-lenineto.socket",
		// not ours: upstream Podman's helper, Homebrew's socket_vmnet, other io.cyber5 products, anything else
		"Library/LaunchDaemons/com.github.containers.podman.helper-lenineto.plist",
		"Library/LaunchDaemons/io.cyber5.comms.plist",
		"Library/LaunchAgents/io.cyber5.kompozi.sync.plist",
		"Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist",
		"Library/LaunchAgents/com.example.other.plist",
		"private/var/run/podman-helper-lenineto.socket",
	)
	calls, out := cleanLaunchItems(t, root, "501")

	want := []string{
		"Library/LaunchAgents/com.example.other.plist",
		"Library/LaunchAgents/io.cyber5.kompozi.sync.plist",
		"Library/LaunchDaemons/com.github.containers.podman.helper-lenineto.plist",
		"Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist",
		"Library/LaunchDaemons/io.cyber5.comms.plist",
		"private/var/run/podman-helper-lenineto.socket",
	}
	if got := remaining(t, root); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("left behind:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, c := range []string{
		"bootout gui/501/io.cyber5.tainer.menu",
		"bootout gui/501/tainer-helper",
		"bootout system/io.cyber5.cyberstack.pf",
		"bootout system/com.github.containers.tainer.helper-lenineto",
	} {
		if !contains(calls, c) {
			t.Errorf("missing launchctl %q in %v", c, calls)
		}
	}
	for _, c := range calls {
		if strings.Contains(c, "podman") || strings.Contains(c, "socket_vmnet") || strings.Contains(c, "example") ||
			strings.Contains(c, "comms") || strings.Contains(c, "kompozi") {
			t.Errorf("booted out something not ours: %q", c)
		}
	}
	if !strings.Contains(out, "removed "+root+"/Library/LaunchDaemons/com.github.containers.tainer.helper-lenineto.plist") {
		t.Errorf("output does not name what it removed:\n%s", out)
	}
}

func TestUninstallLaunchItemsIsIdempotent(t *testing.T) {
	root := t.TempDir()
	if _, out := cleanLaunchItems(t, root, "501"); strings.Contains(out, "removed") {
		t.Errorf("nothing to remove, but: %s", out)
	}
	// No console user: daemons still go, agents' gui bootouts are skipped.
	touch(t, root, "Library/LaunchDaemons/io.cyber5.cyberstack.x.plist", "Library/LaunchAgents/io.cyber5.tainer.y.plist")
	calls, _ := cleanLaunchItems(t, root, "")
	if len(remaining(t, root)) != 0 {
		t.Errorf("left %v", remaining(t, root))
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "bootout gui/") {
			t.Errorf("gui bootout without a user: %q", c)
		}
	}
}

func TestSourcingTheUninstallerRunsNothing(t *testing.T) {
	script, _ := filepath.Abs("tainer-uninstall.sh")
	out, err := exec.Command("bash", "-c", `source "$1"; type clean_launch_items >/dev/null && echo defined`, "test", script).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "defined" {
		t.Errorf("sourcing printed or failed: %v %q", err, out)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// launchctl needs the Label inside the plist, which need not match the file name.
func TestUninstallBootsOutByThePlistsLabel(t *testing.T) {
	root := t.TempDir()
	write := func(rel, label string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>` + label + `</string></dict></plist>
`
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Library/LaunchDaemons/io.cyber5.cyberstack.pf.plist", "io.cyber5.cyberstack.pf-anchor")
	write("Library/LaunchAgents/tainer-old.plist", "io.cyber5.tainer.old-agent")
	touch(t, root, "Library/LaunchDaemons/tainer-unreadable.plist") // no Label: file name
	calls, _ := cleanLaunchItems(t, root, "501")
	for _, c := range []string{
		"bootout system/io.cyber5.cyberstack.pf-anchor",
		"bootout gui/501/io.cyber5.tainer.old-agent",
		"bootout system/tainer-unreadable",
	} {
		if !contains(calls, c) {
			t.Errorf("missing %q in %v", c, calls)
		}
	}
	if len(remaining(t, root)) != 0 {
		t.Errorf("left %v", remaining(t, root))
	}
}
