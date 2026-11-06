package accounts

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientMapsAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"code":"invalid_credentials"}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.Login("a@b.co", "pw", "mac")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 401 || ae.Code != "invalid_credentials" {
		t.Fatalf("got %#v", err)
	}
}

func TestClientLoginRefreshMe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["email"] != "a@b.co" || b["password"] != "pw" || b["device_name"] != "mac" {
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{"access_token":"A","token_type":"Bearer","expires_in":900,"refresh_token":"R"}`))
	})
	mux.HandleFunc("/v1/token", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["refresh_token"] != "R" {
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{"access_token":"A2","token_type":"Bearer","expires_in":900,"refresh_token":"R2"}`))
	})
	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer A2" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"code":"invalid_token"}}`))
			return
		}
		w.Write([]byte(`{"id":"u1","email":"a@b.co","email_verified":true,"created_at":"x"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	tp, err := c.Login("a@b.co", "pw", "mac")
	if err != nil || tp.AccessToken != "A" || tp.RefreshToken != "R" || tp.ExpiresIn != 900 {
		t.Fatalf("%+v %v", tp, err)
	}
	tp, err = c.Refresh("R")
	if err != nil || tp.RefreshToken != "R2" {
		t.Fatalf("%+v %v", tp, err)
	}
	u, err := c.Me(tp.AccessToken)
	if err != nil || u.ID != "u1" || !u.EmailVerified {
		t.Fatalf("%+v %v", u, err)
	}
}

func TestNewClientBaseURL(t *testing.T) {
	t.Setenv("TAINER_ID_URL", "")
	if NewClient().BaseURL != "https://id.tainer.dev" {
		t.Fatal("default")
	}
	t.Setenv("TAINER_ID_URL", "http://x")
	if NewClient().BaseURL != "http://x" {
		t.Fatal("override")
	}
}

func TestClientNonJSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("<html>boom</html>"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.Refresh("r")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 500 || ae.Code != "" {
		t.Fatalf("got %#v", err)
	}
}

func TestClientRejectsEmptyTokenPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if tp, err := c.Login("a", "b", "c"); err == nil {
		t.Fatalf("login accepted %+v", tp)
	}
	if tp, err := c.Refresh("r"); err == nil {
		t.Fatalf("refresh accepted %+v", tp)
	}
}

func TestClientLogoutPostsRefreshToken(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotBody = r.URL.Path, string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.Logout("rt-1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/logout" || gotBody != `{"refresh_token":"rt-1"}` {
		t.Errorf("path %q body %q", gotPath, gotBody)
	}
}

func TestSignupPostsCredentials(t *testing.T) {
	var path string
	var body map[string]string
	status, resp := 200, `{"ok":true,"message":"check your email"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(status)
		w.Write([]byte(resp))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.Signup("a@b.co", "longenough1"); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/signup" || body["email"] != "a@b.co" || body["password"] != "longenough1" {
		t.Errorf("path %q body %v", path, body)
	}
	status, resp = 400, `{"error":{"code":"weak_password"}}`
	var ae *APIError
	if err := c.Signup("a@b.co", "short"); !errors.As(err, &ae) || ae.Code != "weak_password" || ae.Status != 400 {
		t.Fatalf("got %#v", err)
	}
}

func TestRequestPasswordResetPostsEmail(t *testing.T) {
	var path string
	var body map[string]string
	status, resp := 200, `{"ok":true}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(status)
		w.Write([]byte(resp))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.RequestPasswordReset("a@b.co"); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/password-reset/request" || body["email"] != "a@b.co" || len(body) != 1 {
		t.Errorf("path %q body %v", path, body)
	}
	status, resp = 400, `{"error":{"code":"weak_password"}}`
	var ae *APIError
	if err := c.RequestPasswordReset("a@b.co"); !errors.As(err, &ae) || ae.Code != "weak_password" {
		t.Fatalf("got %#v", err)
	}
}
