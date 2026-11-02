package engine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newFakeEngine serves handler on a unix socket and returns a Client dialing it.
func newFakeEngine(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "eng")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "e.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	prev, had := os.LookupEnv("DOCKER_HOST")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("DOCKER_HOST", prev)
		} else {
			_ = os.Unsetenv("DOCKER_HOST")
		}
	})
	_ = os.Setenv("DOCKER_HOST", "unix://"+sock)
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestImageInspect(t *testing.T) {
	const ref = "ghcr.io/cyber5-io/tainer-caddy-web:2-alpine"
	c := newFakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/images/"+ref+"/json") {
			http.Error(w, `{"message":"No such image"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"abc123","Architecture":"arm64"}`))
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := c.ImageInspect(ctx, ref)
	if err != nil {
		t.Fatalf("ImageInspect: %v", err)
	}
	if want := (ImageInfo{ID: "abc123", Architecture: "arm64"}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestImageInspectNotFound(t *testing.T) {
	c := newFakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"No such image"}`, http.StatusNotFound)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.ImageInspect(ctx, "example.com/missing:1")
	if !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("want ErrImageNotFound, got %v", err)
	}
}
