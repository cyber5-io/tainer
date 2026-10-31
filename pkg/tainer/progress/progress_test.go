package progress

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type capture struct {
	method, path, ctype string
	body                []byte
}

func serveUnix(t *testing.T, status int) (<-chan capture, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "tp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan capture, 4)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- capture{r.Method, r.URL.Path, r.Header.Get("Content-Type"), b}
		w.WriteHeader(status)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return got, sock
}

func TestReporterPostsShape(t *testing.T) {
	got, sock := serveUnix(t, http.StatusNoContent)
	t.Setenv("DOCKER_HOST", "unix://"+sock)

	report := Reporter(context.Background(), "start", "demo")
	report("pull", "pulling images", -1, false)

	select {
	case c := <-got:
		if c.method != http.MethodPost || c.path != "/cyberstack/progress" {
			t.Fatalf("request = %s %s", c.method, c.path)
		}
		if c.ctype != "application/json" {
			t.Fatalf("content-type = %q", c.ctype)
		}
		var doc map[string]any
		if err := json.Unmarshal(c.body, &doc); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"op": "start", "pod": "demo", "step": "pull", "percent": float64(-1), "message": "pulling images", "done": false}
		if len(doc) != len(want) {
			t.Fatalf("doc = %v, want %v", doc, want)
		}
		for k, v := range want {
			if doc[k] != v {
				t.Fatalf("doc[%s] = %v, want %v", k, doc[k], v)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no POST received")
	}
}

func TestReporterSilentWhenUnavailable(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/dir/none.sock")
	report := Reporter(context.Background(), "init", "demo")
	start := time.Now()
	report("scaffold", "x", -1, true)
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("took %v", d)
	}
}

func TestReporterSwallowsRejection(t *testing.T) {
	got, sock := serveUnix(t, http.StatusBadRequest)
	t.Setenv("DOCKER_HOST", "unix://"+sock)
	Reporter(context.Background(), "update", "demo")("pull", "m", 50, false)
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("no POST received")
	}
}
