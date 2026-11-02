package imagecheck

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		in      string
		want    Ref
		wantErr string
	}{
		{"ghcr.io/cyber5-io/tainer-caddy-web:2-alpine", Ref{"ghcr.io", "cyber5-io/tainer-caddy-web", "2-alpine"}, ""},
		{"ghcr.io/cyber5-io/tainer-caddy-web", Ref{"ghcr.io", "cyber5-io/tainer-caddy-web", "latest"}, ""},
		{"localhost:5000/a/b:v1", Ref{"localhost:5000", "a/b", "v1"}, ""},
		{"ghcr.io/a/b@sha256:abcd", Ref{}, "digest"},
		{"nohost", Ref{}, "invalid"},
		{"", Ref{}, "invalid"},
	}
	for _, c := range cases {
		got, err := ParseRef(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("ParseRef(%q) err = %v, want containing %q", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseRef(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
}

const (
	repo      = "cyber5-io/tainer-caddy-web"
	idxDigest = "sha256:" + "1111111111111111111111111111111111111111111111111111111111111111"
	amdMan    = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	armMan    = "sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	amdCfg    = "sha256:" + "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	armCfg    = "sha256:" + "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

type fake struct {
	srv       *httptest.Server
	needAuth  bool
	tokenHits int
	single    bool
	missing   bool
	accept    string
}

func newFake(t *testing.T, f *fake) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.tokenHits++
		q := r.URL.Query()
		if q.Get("service") != "ghcr.io" || q.Get("scope") != "repository:"+repo+":pull" {
			http.Error(w, "bad query", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "t"})
	})
	mux.HandleFunc("/v2/"+repo+"/manifests/", func(w http.ResponseWriter, r *http.Request) {
		if f.needAuth && r.Header.Get("Authorization") != "Bearer t" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+f.srv.URL+`/token",service="ghcr.io",scope="repository:`+repo+`:pull"`)
			w.WriteHeader(401)
			return
		}
		if f.missing {
			w.WriteHeader(404)
			return
		}
		f.accept = r.Header.Get("Accept")
		ref := strings.TrimPrefix(r.URL.Path, "/v2/"+repo+"/manifests/")
		var body any
		switch {
		case ref == "2-alpine" && f.single:
			body = map[string]any{"config": map[string]string{"digest": armCfg}}
		case ref == "2-alpine":
			body = map[string]any{"manifests": []any{
				map[string]any{"digest": amdMan, "platform": map[string]string{"architecture": "amd64", "os": "linux"}},
				map[string]any{"digest": "sha256:unk", "platform": map[string]string{"architecture": "unknown", "os": "unknown"}},
				map[string]any{"digest": armMan, "platform": map[string]string{"architecture": "arm64", "os": "linux"}},
			}}
		case ref == amdMan:
			body = map[string]any{"config": map[string]string{"digest": amdCfg}}
		case ref == armMan:
			body = map[string]any{"config": map[string]string{"digest": armCfg}}
		default:
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
}

func refFor(f *fake) Ref {
	return Ref{Host: strings.TrimPrefix(f.srv.URL, "http://"), Repo: repo, Tag: "2-alpine"}
}

func client(f *fake) *http.Client { return f.srv.Client() }

func TestRemoteConfigDigestPicksArch(t *testing.T) {
	f := &fake{}
	newFake(t, f)
	for arch, want := range map[string]string{"arm64": armCfg, "amd64": amdCfg, "": armCfg} {
		got, err := remoteConfigDigest(context.Background(), client(f), "http", refFor(f), arch)
		if err != nil || got != want {
			t.Errorf("arch %q = %q, %v; want %q", arch, got, err, want)
		}
	}
	for _, mt := range []string{"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json", "application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"} {
		if !strings.Contains(f.accept, mt) {
			t.Errorf("Accept %q missing %s", f.accept, mt)
		}
	}
}

func TestRemoteConfigDigestSingleManifest(t *testing.T) {
	f := &fake{single: true}
	newFake(t, f)
	got, err := remoteConfigDigest(context.Background(), client(f), "http", refFor(f), "arm64")
	if err != nil || got != armCfg {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRemoteConfigDigestNotFound(t *testing.T) {
	f := &fake{missing: true}
	newFake(t, f)
	_, err := remoteConfigDigest(context.Background(), client(f), "http", refFor(f), "arm64")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRemoteConfigDigestTokenFlow(t *testing.T) {
	f := &fake{needAuth: true}
	newFake(t, f)
	got, err := remoteConfigDigest(context.Background(), client(f), "http", refFor(f), "arm64")
	if err != nil || got != armCfg {
		t.Fatalf("got %q, %v", got, err)
	}
	if f.tokenHits != 1 {
		t.Errorf("token hits = %d, want 1 (token reused)", f.tokenHits)
	}
}

func TestRemoteConfigDigestNoMatchingArch(t *testing.T) {
	f := &fake{}
	newFake(t, f)
	if _, err := remoteConfigDigest(context.Background(), client(f), "http", refFor(f), "riscv64"); err == nil {
		t.Fatal("want error for missing platform")
	}
}
