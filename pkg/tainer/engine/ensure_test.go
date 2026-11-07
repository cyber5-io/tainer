package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func fakes(local map[string]bool, pullErr error) (func(context.Context, string) bool, func(context.Context, string) error, *[]string) {
	var pulled []string
	has := func(_ context.Context, ref string) bool { return local[ref] }
	pull := func(_ context.Context, ref string) error {
		pulled = append(pulled, ref)
		return pullErr
	}
	return has, pull, &pulled
}

func TestEnsureImagesOfflineAllLocalNeverPulls(t *testing.T) {
	has, pull, pulled := fakes(map[string]bool{"a:1": true, "b:1": true}, errors.New("offline"))
	if err := EnsureImages(context.Background(), []string{"a:1", "b:1"}, has, pull); err != nil {
		t.Fatal(err)
	}
	if len(*pulled) != 0 {
		t.Errorf("pulled %v", *pulled)
	}
}

func TestEnsureImagesOfflineMissingFailsPlainly(t *testing.T) {
	has, pull, _ := fakes(map[string]bool{"a:1": true}, errors.New("docker://x: podman engine VM dial tcp"))
	err := EnsureImages(context.Background(), []string{"a:1", "b:1"}, has, pull)
	if err == nil {
		t.Fatal("want error")
	}
	if want := "couldn't download b:1 — are you offline?"; err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
	var de *DownloadError
	if !errors.As(err, &de) {
		t.Error("want DownloadError")
	}
}

func TestEnsureImagesOnlinePullsOnlyMissing(t *testing.T) {
	has, pull, pulled := fakes(map[string]bool{"a:1": true}, nil)
	if err := EnsureImages(context.Background(), []string{"a:1", "b:1"}, has, pull); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*pulled, ",") != "b:1" {
		t.Errorf("pulled %v, want [b:1]", *pulled)
	}
}
