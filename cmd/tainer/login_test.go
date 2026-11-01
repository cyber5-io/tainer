package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/accounts"
)

type idStub struct {
	srv        *httptest.Server
	loginBody  map[string]string
	logoutHits int
	logoutBody string
}

// newIDStub serves a minimal id service. loginStatus/loginCode make /v1/login
// fail; refreshCode makes /v1/token fail with that error code (401).
func newIDStub(t *testing.T, loginStatus int, loginCode, refreshCode string) *idStub {
	t.Helper()
	st := &idStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&st.loginBody)
		if loginStatus != 0 {
			w.WriteHeader(loginStatus)
			_, _ = w.Write([]byte(`{"error":{"code":"` + loginCode + `"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"at-1","expires_in":900,"refresh_token":"rt-1"}`))
	})
	mux.HandleFunc("/v1/token", func(w http.ResponseWriter, r *http.Request) {
		if refreshCode != "" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"code":"` + refreshCode + `"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"at-2","expires_in":900,"refresh_token":"rt-2"}`))
	})
	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u-1","email":"leni@example.com","email_verified":true}`))
	})
	mux.HandleFunc("/v1/logout", func(w http.ResponseWriter, r *http.Request) {
		st.logoutHits++
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(r.Body)
		st.logoutBody = b.String()
		w.WriteHeader(http.StatusNoContent)
	})
	st.srv = httptest.NewServer(mux)
	t.Cleanup(st.srv.Close)
	return st
}

func (s *idStub) client() *accounts.Client {
	return &accounts.Client{BaseURL: s.srv.URL, HTTP: s.srv.Client()}
}

func decodeDoc(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if m["v"] != float64(1) {
		t.Errorf("missing v:1 in %s", out.String())
	}
	return m
}

func errCodeOf(t *testing.T, out *bytes.Buffer) string {
	t.Helper()
	m := decodeDoc(t, out)
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func TestLoginJSONReadsStdinAndStoresSession(t *testing.T) {
	st := newIDStub(t, 0, "", "")
	dir := t.TempDir()
	var out bytes.Buffer
	in := strings.NewReader(`{"email":"leni@example.com","password":"s3cret","deviceName":"my mac"}`)
	if code := runLogin([]string{"--json"}, in, &out, dir, st.client()); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	doc := decodeDoc(t, &out)
	if doc["loggedIn"] != true || doc["userId"] != "u-1" || doc["email"] != "leni@example.com" {
		t.Errorf("doc %v", doc)
	}
	if strings.Contains(out.String(), "at-1") || strings.Contains(out.String(), "rt-1") || strings.Contains(out.String(), "s3cret") {
		t.Errorf("output leaks secrets: %s", out.String())
	}
	if st.loginBody["device_name"] != "my mac" || st.loginBody["password"] != "s3cret" {
		t.Errorf("login body %v", st.loginBody)
	}
	s, err := accounts.Load(dir)
	if err != nil || s == nil || s.RefreshToken != "rt-1" || s.UserID != "u-1" {
		t.Errorf("stored session %+v err %v", s, err)
	}
}

func TestLoginJSONDefaultsDeviceName(t *testing.T) {
	st := newIDStub(t, 0, "", "")
	var out bytes.Buffer
	in := strings.NewReader(`{"email":"a@b.co","password":"pw"}`)
	if code := runLogin([]string{"--json"}, in, &out, t.TempDir(), st.client()); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.HasSuffix(st.loginBody["device_name"], " · tainer") {
		t.Errorf("device name %q", st.loginBody["device_name"])
	}
}

func TestLoginJSONBadCredentials(t *testing.T) {
	for _, tc := range []struct {
		status     int
		code, slug string
	}{
		{401, "invalid_credentials", "login-failed"},
		{403, "email_unverified", "email-unverified"},
		{429, "too_many_attempts", "too-many-attempts"},
		{503, "", "id-unreachable"},
	} {
		st := newIDStub(t, tc.status, tc.code, "")
		dir := t.TempDir()
		var out bytes.Buffer
		in := strings.NewReader(`{"email":"a@b.co","password":"bad"}`)
		if code := runLogin([]string{"--json"}, in, &out, dir, st.client()); code != 1 {
			t.Fatalf("%s: exit %d", tc.slug, code)
		}
		if got := errCodeOf(t, &out); got != tc.slug {
			t.Errorf("status %d: slug %q want %q", tc.status, got, tc.slug)
		}
		if s, _ := accounts.Load(dir); s != nil {
			t.Errorf("%s: session stored on failure", tc.slug)
		}
	}
}

func TestLoginJSONUnreachableAndMalformedStdin(t *testing.T) {
	st := newIDStub(t, 0, "", "")
	c := st.client()
	st.srv.Close()
	var out bytes.Buffer
	if code := runLogin([]string{"--json"}, strings.NewReader(`{"email":"a@b.co","password":"pw"}`), &out, t.TempDir(), c); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if got := errCodeOf(t, &out); got != "id-unreachable" {
		t.Errorf("slug %q", got)
	}
	for _, bad := range []string{``, `not json`, `{"email":"a@b.co"}`, `{"password":"pw"}`} {
		out.Reset()
		if code := runLogin([]string{"--json"}, strings.NewReader(bad), &out, t.TempDir(), c); code != 1 {
			t.Fatalf("%q: exit %d", bad, code)
		}
		if got := errCodeOf(t, &out); got != "invalid-arguments" {
			t.Errorf("%q: slug %q", bad, got)
		}
	}
}

func TestWhoamiLoggedOutDocument(t *testing.T) {
	var out bytes.Buffer
	if code := runWhoami([]string{"--json"}, &out, t.TempDir(), newIDStub(t, 0, "", "").client()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := out.String(); got != "{\n  \"v\": 1,\n  \"loggedIn\": false\n}\n" {
		t.Errorf("got %q", got)
	}
}

func saveSession(t *testing.T, dir string, expiresIn time.Duration) {
	t.Helper()
	err := accounts.Save(dir, &accounts.Session{Email: "leni@example.com", UserID: "u-1", RefreshToken: "rt-0", AccessToken: "at-0", AccessTokenExpiresAt: time.Now().Add(expiresIn)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWhoamiReturnsFreshStoredToken(t *testing.T) {
	dir := t.TempDir()
	saveSession(t, dir, 10*time.Minute)
	var out bytes.Buffer
	if code := runWhoami([]string{"--json"}, &out, dir, newIDStub(t, 0, "", "").client()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	doc := decodeDoc(t, &out)
	if doc["loggedIn"] != true || doc["accessToken"] != "at-0" || doc["userId"] != "u-1" || doc["email"] != "leni@example.com" {
		t.Errorf("doc %v", doc)
	}
	exp, _ := doc["accessTokenExpiresAt"].(string)
	if _, err := time.Parse(time.RFC3339, exp); err != nil || !strings.HasSuffix(exp, "Z") || len(exp) != len("2026-11-01T09:00:00.000Z") {
		t.Errorf("accessTokenExpiresAt %q", exp)
	}
	if _, has := doc["offline"]; has {
		t.Error("offline must be absent when online")
	}
}

func TestWhoamiRefreshesExpiredToken(t *testing.T) {
	dir := t.TempDir()
	saveSession(t, dir, -time.Minute)
	var out bytes.Buffer
	if code := runWhoami([]string{"--json"}, &out, dir, newIDStub(t, 0, "", "").client()); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	doc := decodeDoc(t, &out)
	if doc["accessToken"] != "at-2" {
		t.Errorf("doc %v", doc)
	}
	if s, _ := accounts.Load(dir); s == nil || s.RefreshToken != "rt-2" {
		t.Errorf("rotated pair not saved: %+v", s)
	}
}

func TestWhoamiDeadSessionClearsStore(t *testing.T) {
	for _, code := range []string{"session_expired", "session_revoked", "invalid_token"} {
		dir := t.TempDir()
		saveSession(t, dir, -time.Minute)
		var out bytes.Buffer
		if exit := runWhoami([]string{"--json"}, &out, dir, newIDStub(t, 0, "", code).client()); exit != 0 {
			t.Fatalf("%s: exit %d", code, exit)
		}
		if doc := decodeDoc(t, &out); doc["loggedIn"] != false {
			t.Errorf("%s: doc %v", code, doc)
		}
		if s, _ := accounts.Load(dir); s != nil {
			t.Errorf("%s: store not cleared", code)
		}
	}
}

func TestWhoamiOfflineFallsBackToStoredToken(t *testing.T) {
	dir := t.TempDir()
	saveSession(t, dir, 30*time.Second) // inside the refresh skew, still unexpired
	st := newIDStub(t, 0, "", "")
	c := st.client()
	st.srv.Close()
	var out bytes.Buffer
	if code := runWhoami([]string{"--json"}, &out, dir, c); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	doc := decodeDoc(t, &out)
	if doc["loggedIn"] != true || doc["offline"] != true || doc["accessToken"] != "at-0" {
		t.Errorf("doc %v", doc)
	}
	if s, _ := accounts.Load(dir); s == nil {
		t.Error("offline must not clear the store")
	}
}

func TestWhoamiOfflineWithExpiredTokenIsUnreachable(t *testing.T) {
	dir := t.TempDir()
	saveSession(t, dir, -time.Minute)
	st := newIDStub(t, 0, "", "")
	c := st.client()
	st.srv.Close()
	var out bytes.Buffer
	if code := runWhoami([]string{"--json"}, &out, dir, c); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if got := errCodeOf(t, &out); got != "id-unreachable" {
		t.Errorf("slug %q", got)
	}
	if s, _ := accounts.Load(dir); s == nil {
		t.Error("store must survive an outage")
	}
}

func TestLogoutClearsStore(t *testing.T) {
	dir := t.TempDir()
	saveSession(t, dir, 10*time.Minute)
	st := newIDStub(t, 0, "", "")
	var out bytes.Buffer
	if code := runLogout([]string{"--json"}, &out, dir, st.client()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := out.String(); got != "{\n  \"v\": 1,\n  \"loggedOut\": true\n}\n" {
		t.Errorf("got %q", got)
	}
	if st.logoutHits != 1 || st.logoutBody != `{"refresh_token":"rt-0"}` {
		t.Errorf("hits %d body %q", st.logoutHits, st.logoutBody)
	}
	if s, _ := accounts.Load(dir); s != nil {
		t.Error("store not cleared")
	}
}

func TestLogoutClearsEvenWhenServiceDown(t *testing.T) {
	dir := t.TempDir()
	saveSession(t, dir, 10*time.Minute)
	st := newIDStub(t, 0, "", "")
	c := st.client()
	st.srv.Close()
	var out bytes.Buffer
	if code := runLogout([]string{"--json"}, &out, dir, c); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if s, _ := accounts.Load(dir); s != nil {
		t.Error("store not cleared")
	}
}

func TestLogoutWhenLoggedOut(t *testing.T) {
	var out bytes.Buffer
	if code := runLogout([]string{"--json"}, &out, t.TempDir(), newIDStub(t, 0, "", "").client()); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if decodeDoc(t, &out)["loggedOut"] != true {
		t.Errorf("got %s", out.String())
	}
}
