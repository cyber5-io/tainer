package controldev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return t.TempDir()
}

func TestNothingApprovedByDefault(t *testing.T) {
	dir := setup(t)
	if Approved(dir) {
		t.Fatal("fresh config approves nothing")
	}
	got, err := List()
	if err != nil || len(got) != 0 {
		t.Fatalf("List = %v, %v; want empty", got, err)
	}
}

func TestAllowApprovesThatPathOnly(t *testing.T) {
	dir := setup(t)
	other := t.TempDir()
	if err := Allow(dir); err != nil {
		t.Fatal(err)
	}
	if !Approved(dir) {
		t.Error("allowed path not approved")
	}
	if !Approved(dir + "/") {
		t.Error("trailing slash must resolve to the same folder")
	}
	if Approved(other) {
		t.Error("approval is path-specific")
	}
	if Approved(filepath.Join(dir, "sub")) {
		t.Error("a subfolder is not the approved folder")
	}
	if Approved("") {
		t.Error("empty path is never approved")
	}
}

func TestAllowIsIdempotentAndListed(t *testing.T) {
	dir := setup(t)
	for i := 0; i < 2; i++ {
		if err := Allow(dir); err != nil {
			t.Fatal(err)
		}
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := Canonical(dir)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("List = %v, want [%s]", got, want)
	}
}

func TestFileFormat(t *testing.T) {
	dir := setup(t)
	if err := Allow(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		V        int      `json:"v"`
		Approved []string `json:"approved"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	want, _ := Canonical(dir)
	if f.V != 1 || len(f.Approved) != 1 || f.Approved[0] != want || !filepath.IsAbs(f.Approved[0]) {
		t.Fatalf("file = %s", b)
	}
	if _, err := os.Stat(Path() + ".partial"); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
}

func TestRevokeRemovesThePath(t *testing.T) {
	dir := setup(t)
	other := t.TempDir()
	if err := Allow(dir); err != nil {
		t.Fatal(err)
	}
	if err := Allow(other); err != nil {
		t.Fatal(err)
	}
	if err := Revoke(dir); err != nil {
		t.Fatal(err)
	}
	if Approved(dir) {
		t.Error("revoked path still approved")
	}
	if !Approved(other) {
		t.Error("revoke removed the wrong path")
	}
}

func TestRevokeUnknownPathIsAnError(t *testing.T) {
	dir := setup(t)
	if err := Revoke(dir); err == nil {
		t.Fatal("revoking a folder that isn't approved must say so")
	}
}

func TestRevokeWorksForAMissingFolder(t *testing.T) {
	dir := setup(t)
	gone := filepath.Join(dir, "checkout")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Allow(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if err := Revoke(gone); err != nil {
		t.Fatalf("revoke of a deleted checkout: %v", err)
	}
	if n, _ := List(); len(n) != 0 {
		t.Fatalf("still listed: %v", n)
	}
}

func TestCorruptFileApprovesNothing(t *testing.T) {
	dir := setup(t)
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Approved(dir) {
		t.Fatal("unreadable approvals must approve nothing")
	}
	if err := Allow(dir); err == nil {
		t.Fatal("allow must not silently overwrite an unreadable approvals file")
	}
}

func TestConcurrentChangesAreNeverLost(t *testing.T) {
	setup(t)
	const n = 40
	dirs := make([]string, n)
	for i := range dirs {
		dirs[i] = t.TempDir()
	}
	// Half start approved and get revoked, half get approved, all at once.
	for _, d := range dirs[:n/2] {
		if err := Allow(d); err != nil {
			t.Fatal(err)
		}
	}
	errs := make(chan error, n)
	for i, d := range dirs {
		go func(i int, d string) {
			if i < n/2 {
				errs <- Revoke(d)
			} else {
				errs <- Allow(d)
			}
		}(i, d)
	}
	// Drain every result before failing: returning early would restore
	// XDG_CONFIG_HOME while goroutines still write, sending them to the real config.
	var first error
	for range dirs {
		if err := <-errs; err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		t.Fatal(first)
	}
	for i, d := range dirs {
		if got, want := Approved(d), i >= n/2; got != want {
			t.Errorf("%d: approved=%v, want %v (a concurrent change was lost)", i, got, want)
		}
	}
}
