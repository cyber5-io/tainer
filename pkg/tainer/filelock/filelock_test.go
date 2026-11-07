package filelock

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLockExcludesAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.lock")
	release, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := TryLock(path); ok || err != nil {
		t.Fatalf("TryLock while held: ok=%v err=%v", ok, err)
	}
	got := make(chan struct{})
	go func() {
		r, err := Lock(path)
		if err == nil {
			r()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("Lock returned while another holder had it")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("Lock never returned after release")
	}
}
