package accounts

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTripAndMode(t *testing.T) {
	dir := t.TempDir()
	want := &Session{
		Email:                "a@b.co",
		UserID:               "u1",
		RefreshToken:         "r1",
		AccessToken:          "a1",
		AccessTokenExpiresAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != *want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	fi, err := os.Stat(filepath.Join(dir, "account.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestLoadMissing(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope"))
	if s != nil || err != nil {
		t.Fatalf("got %v %v", s, err)
	}
}

func TestClearIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, &Session{Email: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if s, _ := Load(dir); s != nil {
		t.Fatal("still present")
	}
}
