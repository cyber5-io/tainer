package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/accounts"
)

type signupStub struct {
	srv    *httptest.Server
	paths  []string
	bodies []map[string]string
}

func newSignupStub(t *testing.T, status int, code string) *signupStub {
	t.Helper()
	st := &signupStub{}
	st.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		st.paths = append(st.paths, r.URL.Path)
		st.bodies = append(st.bodies, b)
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"` + code + `"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"message":"check your email"}`))
	}))
	t.Cleanup(st.srv.Close)
	return st
}

func (s *signupStub) client() *accounts.Client {
	return &accounts.Client{BaseURL: s.srv.URL, HTTP: s.srv.Client()}
}

func TestSignupJSONReadsStdin(t *testing.T) {
	st := newSignupStub(t, 0, "")
	dir := t.TempDir()
	t.Setenv("TAINER_ACCOUNTS_DIR", dir)
	var out bytes.Buffer
	in := strings.NewReader(`{"email":" leni@example.com ","password":"correct-horse"}`)
	if code := runSignup([]string{"--json"}, in, &out, st.client()); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	doc := decodeDoc(t, &out)
	if doc["signedUp"] != true {
		t.Errorf("doc %v", doc)
	}
	if strings.Contains(out.String(), "correct-horse") {
		t.Errorf("output leaks the password: %s", out.String())
	}
	if len(st.paths) != 1 || st.paths[0] != "/v1/signup" || st.bodies[0]["email"] != "leni@example.com" || st.bodies[0]["password"] != "correct-horse" {
		t.Errorf("requests %v %v", st.paths, st.bodies)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("signup wrote to the accounts dir: %v", ents)
	}
}

func TestSignupJSONWeakPassword(t *testing.T) {
	for _, tc := range []struct {
		status     int
		code, slug string
	}{
		{400, "weak_password", "weak-password"},
		{400, "password_too_long", "password-too-long"},
		{400, "invalid_request", "command-failed"},
	} {
		st := newSignupStub(t, tc.status, tc.code)
		var out bytes.Buffer
		if code := runSignup([]string{"--json"}, strings.NewReader(`{"email":"a@b.co","password":"x"}`), &out, st.client()); code != 1 {
			t.Fatalf("%s: exit %d", tc.slug, code)
		}
		if got := errCodeOf(t, &out); got != tc.slug {
			t.Errorf("%s: slug %q", tc.code, got)
		}
	}
}

func TestSignupRateLimited(t *testing.T) {
	st := newSignupStub(t, 429, "rate_limited")
	var out bytes.Buffer
	if code := runSignup([]string{"--json"}, strings.NewReader(`{"email":"a@b.co","password":"longenough1"}`), &out, st.client()); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if got := errCodeOf(t, &out); got != "too-many-attempts" {
		t.Errorf("slug %q", got)
	}
}

func TestSignupBadInputAndUnreachable(t *testing.T) {
	st := newSignupStub(t, 0, "")
	c := st.client()
	for _, bad := range []string{``, `not json`, `{"email":"a@b.co"}`, `{"password":"pw"}`} {
		var out bytes.Buffer
		if code := runSignup([]string{"--json"}, strings.NewReader(bad), &out, c); code != 1 {
			t.Fatalf("%q: exit %d", bad, code)
		}
		if got := errCodeOf(t, &out); got != "invalid-arguments" {
			t.Errorf("%q: slug %q", bad, got)
		}
	}
	if len(st.paths) != 0 {
		t.Errorf("bad input reached the service: %v", st.paths)
	}
	st.srv.Close()
	var out bytes.Buffer
	if code := runSignup([]string{"--json"}, strings.NewReader(`{"email":"a@b.co","password":"longenough1"}`), &out, c); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if got := errCodeOf(t, &out); got != "id-unreachable" {
		t.Errorf("slug %q", got)
	}
}

func TestSignupInteractivePasswordMismatch(t *testing.T) {
	st := newSignupStub(t, 0, "")
	if code := runSignup(nil, strings.NewReader("a@b.co\nlongenough1\nlongenough2\n"), &bytes.Buffer{}, st.client()); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if len(st.paths) != 0 {
		t.Errorf("mismatch reached the service")
	}
	var out bytes.Buffer
	if code := runSignup(nil, strings.NewReader("a@b.co\nlongenough1\nlongenough1\n"), &out, st.client()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := out.String(); got != "Check your email to confirm your address.\n" {
		t.Errorf("got %q", got)
	}
}

func TestPasswordResetJSON(t *testing.T) {
	st := newSignupStub(t, 0, "")
	var out bytes.Buffer
	if code := runPasswordReset([]string{"leni@example.com", "--json"}, &out, st.client()); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if decodeDoc(t, &out)["requested"] != true {
		t.Errorf("got %s", out.String())
	}
	if len(st.paths) != 1 || st.paths[0] != "/v1/password-reset/request" || st.bodies[0]["email"] != "leni@example.com" {
		t.Errorf("requests %v %v", st.paths, st.bodies)
	}
	for _, args := range [][]string{{"--json"}, {"a@b.co", "extra", "--json"}} {
		out.Reset()
		if code := runPasswordReset(args, &out, st.client()); code != 1 {
			t.Fatalf("%v: exit %d", args, code)
		}
		if got := errCodeOf(t, &out); got != "invalid-arguments" {
			t.Errorf("%v: slug %q", args, got)
		}
	}
	out.Reset()
	st429 := newSignupStub(t, 429, "rate_limited")
	if code := runPasswordReset([]string{"a@b.co", "--json"}, &out, st429.client()); code != 1 || errCodeOf(t, &out) != "too-many-attempts" {
		t.Errorf("rate limit: %s", out.String())
	}
}

func TestPasswordResetHumanOutput(t *testing.T) {
	var out bytes.Buffer
	if code := runPasswordReset([]string{"a@b.co"}, &out, newSignupStub(t, 0, "").client()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := out.String(); got != "If that address has an account, a reset link is on its way.\n" {
		t.Errorf("got %q", got)
	}
}

func TestPasswordResetUnreachable(t *testing.T) {
	st := newSignupStub(t, 0, "")
	c := st.client()
	st.srv.Close()
	var out bytes.Buffer
	if code := runPasswordReset([]string{"a@b.co", "--json"}, &out, c); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if got := errCodeOf(t, &out); got != "id-unreachable" {
		t.Errorf("slug %q", got)
	}
}
