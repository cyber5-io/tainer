package release

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
)

func TestResumeRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, ok, err := ReadResume(); ok || err != nil {
		t.Fatalf("empty read: ok=%v err=%v", ok, err)
	}
	in := ResumeFile{
		ForVersion: "1.5.0", FromVersion: "1.4.0",
		WrittenAt: time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC),
		Pods:      []ResumeEntry{{Name: "a", Path: "/p/a"}, {Name: "b", Path: "/p/b"}},
	}
	if err := WriteResume(in); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(config.ResumeFile())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
	out, ok, err := ReadResume()
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestClearResumeIsIdempotent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := ClearResume(); err != nil {
		t.Fatal(err)
	}
	if err := WriteResume(ResumeFile{ForVersion: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := ClearResume(); err != nil {
		t.Fatal(err)
	}
	if err := ClearResume(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := ReadResume(); ok {
		t.Fatal("still present")
	}
}

func TestResumeLockIsExclusive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	entered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- WithResumeLock(func() error {
			close(entered)
			<-releaseFirst
			return nil
		})
	}()
	<-entered
	second := make(chan struct{})
	go func() {
		_ = WithResumeLock(func() error { return nil })
		close(second)
	}()
	select {
	case <-second:
		t.Fatal("second locker ran while first held the lock")
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("second locker never ran")
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}
