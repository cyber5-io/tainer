package pod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// realDir returns a fresh temp dir with every link in its path resolved
// (macOS temp dirs live under the /var -> /private/var link).
func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func wpManifest(mounts ...string) *manifest.Manifest {
	return &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeWordPress, Name: "demo"},
		Runtime: manifest.RuntimeConfig{Database: manifest.DatabaseMariaDB},
		Mounts:  mounts,
	}
}

var allRoles = []string{RoleWeb, RoleApp, RoleDB}

// A cloned project that links app/data/db or a custom mount to a folder
// outside the project is refused by every role that mounts it, and
// nothing is created through the link.
func TestBuildMountsRefusesLinkedFolders(t *testing.T) {
	for _, name := range []string{"app", "data", "db", "media"} {
		t.Run(name, func(t *testing.T) {
			proj, outside := realDir(t), realDir(t)
			if err := os.Symlink(outside, filepath.Join(proj, name)); err != nil {
				t.Fatal(err)
			}
			m := wpManifest("media")
			refused := 0
			for _, role := range allRoles {
				mounts, err := buildMounts(m, proj, role)
				if err != nil {
					refused++
					if !strings.Contains(err.Error(), "refusing to mount "+name) || !strings.Contains(err.Error(), "outside the project") {
						t.Errorf("%s: unclear error: %v", role, err)
					}
					assertCleanCopy(t, err.Error())
					continue
				}
				for _, mt := range mounts {
					if strings.HasPrefix(mt.Source, outside) || mt.Source == filepath.Join(proj, name) {
						t.Errorf("%s: mounted the link: %+v", role, mt)
					}
				}
			}
			if refused == 0 {
				t.Errorf("no role refused the linked %s", name)
			}
			if ents, _ := os.ReadDir(outside); len(ents) != 0 {
				t.Errorf("something was created through the link: %v", ents)
			}
		})
	}
}

// A link that points inside the project is still a link: refused.
func TestBuildMountsRefusesInternalLinks(t *testing.T) {
	proj := realDir(t)
	if err := os.Mkdir(filepath.Join(proj, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("public", filepath.Join(proj, "app")); err != nil {
		t.Fatal(err)
	}
	if _, err := buildMounts(wpManifest(), proj, RoleWeb); err == nil || !strings.Contains(err.Error(), "refusing to mount app") {
		t.Errorf("internal app link: err = %v, want refusal", err)
	}
}

// A dangling link must not be followed by the create-missing-folders step:
// MkdirAll would otherwise create the folder wherever the link points.
func TestBuildMountsNeverCreatesThroughDanglingLink(t *testing.T) {
	proj, outside := realDir(t), realDir(t)
	target := filepath.Join(outside, "made-by-tainer")
	if err := os.Symlink(target, filepath.Join(proj, "data")); err != nil {
		t.Fatal(err)
	}
	for _, role := range allRoles {
		_, _ = buildMounts(wpManifest(), proj, role)
	}
	if _, err := os.Lstat(target); err == nil {
		t.Fatal("tainer created a folder through a dangling link")
	}
	if _, err := buildMounts(wpManifest(), proj, RoleWeb); err == nil {
		t.Error("dangling data link was accepted")
	}
}

// Mount names that leave the project are refused even when the manifest
// was built by hand (validation is the first line, this is the second).
func TestBuildMountsRefusesEscapingNames(t *testing.T) {
	proj := realDir(t)
	for _, name := range []string{"..", "../x", "/etc", ".", "a/../.."} {
		if _, err := buildMounts(wpManifest(name), proj, RoleWeb); err == nil {
			t.Errorf("mount name %q accepted", name)
		}
	}
}

// A regular file (or anything that is not a folder) where a mount folder
// belongs is refused rather than handed over as a file mount.
func TestBuildMountsRefusesNonFolders(t *testing.T) {
	proj := realDir(t)
	if err := os.WriteFile(filepath.Join(proj, "data"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildMounts(wpManifest(), proj, RoleWeb); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("file data: err = %v, want 'not a folder' refusal", err)
	}
}

// Legit projects: missing folders are created (clone-then-start) and the
// mounts point at the real folders inside the project.
func TestBuildMountsLegitProjectUnchanged(t *testing.T) {
	proj := realDir(t)
	m := wpManifest("media")
	for _, role := range allRoles {
		mounts, err := buildMounts(m, proj, role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		for _, mt := range mounts {
			if mt.Source != proj && !strings.HasPrefix(mt.Source, proj+"/") && mt.Target != "/etc/ssh/tainer_authorized_keys" {
				t.Errorf("%s: source outside project: %+v", role, mt)
			}
		}
	}
	for _, d := range []string{"app", "data", "db", "media"} {
		fi, err := os.Lstat(filepath.Join(proj, d))
		if err != nil || !fi.IsDir() {
			t.Errorf("%s not created as a folder: %v", d, err)
		}
	}
}

// A project reached through a link (e.g. ~/Sites -> ~/dev/sites) is fine:
// the link is the user's own, above the project. Mounts use the real path.
func TestBuildMountsProjectDirBehindLink(t *testing.T) {
	real := realDir(t)
	link := filepath.Join(realDir(t), "proj")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	mounts, err := buildMounts(wpManifest(), link, RoleApp)
	if err != nil {
		t.Fatalf("project behind a link: %v", err)
	}
	if !hasMount(mounts, real, "/var/www") {
		t.Errorf("whole-project mount should use the real path %s: %+v", real, mounts)
	}
}

// The web edge only reads: every project mount it gets is read-only. The
// app and db containers keep writing (uploads, database files).
func TestBuildMountsWebEdgeIsReadOnly(t *testing.T) {
	proj := realDir(t)
	m := wpManifest("media")
	web, err := buildMounts(m, proj, RoleWeb)
	if err != nil {
		t.Fatal(err)
	}
	if len(web) < 3 {
		t.Fatalf("web mounts: %+v", web)
	}
	for _, mt := range web {
		if !mt.ReadOnly {
			t.Errorf("web edge mount is writable: %+v", mt)
		}
	}
	for _, role := range []string{RoleApp, RoleDB} {
		mounts, err := buildMounts(m, proj, role)
		if err != nil {
			t.Fatal(err)
		}
		for _, mt := range mounts {
			if mt.Target != "/etc/ssh/tainer_authorized_keys" && mt.ReadOnly {
				t.Errorf("%s mount must stay writable: %+v", role, mt)
			}
		}
	}
}

func hasMount(ms []engine.Mount, src, dst string) bool {
	for _, mt := range ms {
		if mt.Source == src && mt.Target == dst {
			return true
		}
	}
	return false
}

func assertCleanCopy(t *testing.T, msg string) {
	t.Helper()
	low := strings.ToLower(msg)
	for _, w := range []string{"podman", "docker", "engine", "container", " vm"} {
		if strings.Contains(low, w) {
			t.Errorf("user-facing message mentions %q: %s", w, msg)
		}
	}
}

// `cd /users/leni/dev/proj && tainer start` works on a case-insensitive
// disk, but the runtime compares bind sources with the shared home folder by
// exact spelling: every mount is sent with the project spelled as on disk.
func TestBuildMountsUseTheOnDiskSpelling(t *testing.T) {
	proj := filepath.Join(realDir(t), "Proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	typed := strings.ToLower(proj)
	if typed == proj {
		typed = strings.ToUpper(proj)
	}
	if _, err := os.Stat(typed); err != nil {
		t.Skip("this disk is case-sensitive")
	}
	for _, role := range allRoles {
		mounts, err := buildMounts(wpManifest(), typed, role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		for _, mt := range mounts {
			if strings.HasPrefix(mt.Source, filepath.Dir(config.PublicKey())) {
				continue
			}
			if !strings.HasPrefix(mt.Source, proj) {
				t.Errorf("%s: mount source %s not spelled as on disk (%s)", role, mt.Source, proj)
			}
		}
	}
}
