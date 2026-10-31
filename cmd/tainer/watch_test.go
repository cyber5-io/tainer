package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

func TestWatchRequiresJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	opened := false
	open := func(context.Context) (io.ReadCloser, error) {
		opened = true
		return nil, nil
	}
	code := runWatch(context.Background(), nil, open, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if opened {
		t.Fatal("stream opened without --json")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay empty, got %q", out.String())
	}
	if got := strings.TrimSpace(errOut.String()); got != "tainer watch requires --json" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestWatchStreamEOF(t *testing.T) {
	lines := []string{
		`{"v":1,"type":"hello","ts":"2026-10-31T10:00:00.000Z"}`,
		`{"v":1,"type":"pod","ts":"2026-10-31T10:00:01.000Z","name":"a"}`,
		`{"v":1,"type":"pod","ts":"2026-10-31T10:00:02.000Z","name":"b"}`,
	}
	sock := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cyberstack/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, l := range lines {
			io.WriteString(w, l+"\n")
			w.(http.Flusher).Flush()
		}
	}))
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	t.Setenv("DOCKER_HOST", "unix://"+sock)
	eng, err := engine.New()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	var out, errOut bytes.Buffer
	code := runWatch(context.Background(), []string{"--json"}, eng.EventsStream, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(got) != 4 {
		t.Fatalf("want 4 lines, got %d: %q", len(got), out.String())
	}
	for i, l := range lines {
		if got[i] != l {
			t.Errorf("line %d = %q, want verbatim %q", i, got[i], l)
		}
	}
	if !strings.HasPrefix(got[3], `{"v":1,"error":{"code":"watch-ended","message":`) {
		t.Errorf("last line = %q", got[3])
	}
}
