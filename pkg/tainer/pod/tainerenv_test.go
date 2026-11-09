package pod

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/env"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func hasEnv(e []string, want string) bool {
	for _, kv := range e {
		if kv == want {
			return true
		}
	}
	return false
}

func envKey(e []string, key string) (string, bool) {
	for _, kv := range e {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func TestBuildEnvHasTainerEnvDev(t *testing.T) {
	pctx := &podCtx{hostUID: 501, hostGID: 20}
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Name: "shop", Type: manifest.TypeWordPress, Domain: "shop.tainer.me"},
		Runtime: manifest.RuntimeConfig{PHP: "8.3", Database: manifest.DatabaseMariaDB},
	}
	for _, role := range RolesForPod(m) {
		e := buildEnv(m, role, pctx)
		if !hasEnv(e, "TAINER_ENV=dev") {
			t.Errorf("%s env missing TAINER_ENV=dev: %v", role, e)
		}
		if v, ok := envKey(e, "TAINER_LIVE_DOMAIN"); ok {
			t.Errorf("%s env has TAINER_LIVE_DOMAIN=%q with no live block", role, v)
		}
	}

	m.Live = &manifest.LiveConfig{Domain: "shop.example.com"}
	for _, role := range RolesForPod(m) {
		e := buildEnv(m, role, pctx)
		if !hasEnv(e, "TAINER_ENV=dev") || !hasEnv(e, "TAINER_LIVE_DOMAIN=shop.example.com") {
			t.Errorf("%s env missing TAINER_ENV/TAINER_LIVE_DOMAIN: %v", role, e)
		}
		if !hasEnv(e, "TAINER_DOMAIN=shop.tainer.me") {
			t.Errorf("%s env lost TAINER_DOMAIN: %v", role, e)
		}
	}
}

func TestManifestHashChangesWithLiveDomain(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Name: "shop", Type: manifest.TypeNodeJS, Domain: "shop.tainer.me"},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabaseNone},
		Pod:     &manifest.PodConfig{Size: manifest.PodSizeSmall},
	}
	none := ManifestHash(m)
	m.Live = &manifest.LiveConfig{Domain: "shop.example.com"}
	a := ManifestHash(m)
	m.Live.Domain = "www.example.com"
	b := ManifestHash(m)
	if none == a || a == b {
		t.Errorf("hash ignores live.domain: none=%s a=%s b=%s", none, a, b)
	}
}

// Start's project check refuses a project that sets TAINER_ENV itself, in
// the root .env or app/.env. TAINER_DOMAIN is tainer's own line in the
// generated .env and must pass.
func TestStartRefusesUserTainerEnv(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.ProjectConfig{Type: manifest.TypeNodeJS, Name: "shop", Domain: "shop.tainer.me"},
		Runtime: manifest.RuntimeConfig{Node: "22", Database: manifest.DatabasePostgres},
	}

	for _, tc := range []struct{ file, body string }{
		{".env", "TAINER_ENV=live\n"},
		{".env", "# mine\nexport TAINER_ENV=staging\n"},
		{".env", "  TAINER_ENV = live\n"},
		{"app/.env", "FOO=1\nTAINER_ENV=live\n"},
	} {
		p := realDir(t)
		writeFile(t, filepath.Join(p, filepath.FromSlash(tc.file)), tc.body)
		err := checkProjectFolders(m, p, &bytes.Buffer{})
		want := "TAINER_ENV is set by tainer — remove it from " + tc.file
		if err == nil || err.Error() != want {
			t.Errorf("%s %q: got %v, want %q", tc.file, tc.body, err, want)
		}
	}

	t.Run("generated .env passes", func(t *testing.T) {
		p := realDir(t)
		creds := env.DBCreds{Name: "tainer", User: "tainer", Password: "p", RootPassword: "r"}
		if err := env.GenerateWithCreds(m, filepath.Join(p, ".env"), creds); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(p, "app", "index.js"), "x")
		if err := env.GenerateAppEnv(m, filepath.Join(p, "app", ".env"), creds); err != nil {
			t.Fatal(err)
		}
		if err := checkProjectFolders(m, p, &bytes.Buffer{}); err != nil {
			t.Fatalf("generated .env refused: %v", err)
		}
	})

	t.Run("comments and look-alikes pass", func(t *testing.T) {
		p := realDir(t)
		writeFile(t, filepath.Join(p, ".env"), "# TAINER_ENV=live\nTAINER_ENVIRONMENT=x\nMY_TAINER_ENV=y\nTAINER_DOMAIN=shop.tainer.me\n")
		if err := checkProjectFolders(m, p, &bytes.Buffer{}); err != nil {
			t.Fatalf("refused: %v", err)
		}
	})

	t.Run("legacy html/.env is checked after the move", func(t *testing.T) {
		p := realDir(t)
		writeFile(t, filepath.Join(p, "html", ".env"), "TAINER_ENV=live\n")
		err := checkProjectFolders(m, p, &bytes.Buffer{})
		if err == nil || err.Error() != "TAINER_ENV is set by tainer — remove it from app/.env" {
			t.Errorf("got %v", err)
		}
	})
}
