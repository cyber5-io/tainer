package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func tokenServer(t *testing.T, calls *int32, handle func(refresh string) (int, string)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		status, body := handle(b["refresh_token"])
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestEnsureLoggedOut(t *testing.T) {
	s, err := Ensure(t.TempDir(), &Client{})
	if s != nil || err != nil {
		t.Fatalf("%v %v", s, err)
	}
}

func TestEnsureFreshTokenNoRefresh(t *testing.T) {
	dir := t.TempDir()
	Save(dir, &Session{Email: "e", RefreshToken: "r", AccessToken: "a", AccessTokenExpiresAt: time.Now().Add(5 * time.Minute)})
	var calls int32
	c := tokenServer(t, &calls, func(string) (int, string) { return 500, "" })
	s, err := Ensure(dir, c)
	if err != nil || s.AccessToken != "a" || calls != 0 {
		t.Fatalf("%+v %v calls=%d", s, err, calls)
	}
}

func TestEnsureRefreshesAndSavesBeforeReturn(t *testing.T) {
	dir := t.TempDir()
	Save(dir, &Session{Email: "e", UserID: "u", RefreshToken: "old", AccessToken: "a", AccessTokenExpiresAt: time.Now().Add(-time.Minute)})
	var calls int32
	c := tokenServer(t, &calls, func(r string) (int, string) {
		if r != "old" {
			return 401, `{"error":{"code":"invalid_token"}}`
		}
		return 200, `{"access_token":"new-a","token_type":"Bearer","expires_in":900,"refresh_token":"new-r"}`
	})
	if _, err := Ensure(dir, c); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	got, _ := Load(dir)
	if got.RefreshToken != "new-r" || got.AccessToken != "new-a" || got.Email != "e" || got.UserID != "u" {
		t.Fatalf("%+v", got)
	}
	if time.Until(got.AccessTokenExpiresAt) < 14*time.Minute {
		t.Fatalf("expiry %v", got.AccessTokenExpiresAt)
	}
}

func TestEnsureRereadsOnInvalidToken(t *testing.T) {
	dir := t.TempDir()
	Save(dir, &Session{Email: "e", RefreshToken: "stale", AccessToken: "a", AccessTokenExpiresAt: time.Now().Add(-time.Minute)})
	var calls int32
	c := tokenServer(t, &calls, func(r string) (int, string) {
		switch r {
		case "stale":
			// another process rotates and saves while our request is in flight
			Save(dir, &Session{Email: "e", RefreshToken: "newer", AccessToken: "a", AccessTokenExpiresAt: time.Now().Add(-time.Minute)})
			return 401, `{"error":{"code":"invalid_token"}}`
		case "newer":
			return 200, `{"access_token":"A3","token_type":"Bearer","expires_in":900,"refresh_token":"R3"}`
		}
		return 500, ""
	})
	s, err := Ensure(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	if s.RefreshToken != "R3" || calls != 2 {
		t.Fatalf("%+v calls=%d", s, calls)
	}
	if got, _ := Load(dir); got.RefreshToken != "R3" {
		t.Fatalf("disk %+v", got)
	}
}

func TestEnsureRereadAdoptsFreshSessionWithoutSecondCall(t *testing.T) {
	dir := t.TempDir()
	Save(dir, &Session{Email: "e", RefreshToken: "stale", AccessToken: "a", AccessTokenExpiresAt: time.Now().Add(-time.Minute)})
	var calls int32
	c := tokenServer(t, &calls, func(r string) (int, string) {
		Save(dir, &Session{Email: "e", RefreshToken: "n", AccessToken: "fresh", AccessTokenExpiresAt: time.Now().Add(10 * time.Minute)})
		return 401, `{"error":{"code":"invalid_token"}}`
	})
	s, err := Ensure(dir, c)
	if err != nil || s.AccessToken != "fresh" || calls != 1 {
		t.Fatalf("%+v %v calls=%d", s, err, calls)
	}
}

func TestEnsureInvalidTokenTwiceFails(t *testing.T) {
	dir := t.TempDir()
	Save(dir, &Session{Email: "e", RefreshToken: "dead", AccessTokenExpiresAt: time.Now().Add(-time.Minute)})
	var calls int32
	c := tokenServer(t, &calls, func(string) (int, string) { return 401, `{"error":{"code":"invalid_token"}}` })
	_, err := Ensure(dir, c)
	ae, ok := err.(*APIError)
	if !ok || ae.Code != "invalid_token" {
		t.Fatalf("%#v", err)
	}
	if calls != 1 {
		t.Fatalf("unchanged file must not be retried; calls=%d", calls)
	}
}
