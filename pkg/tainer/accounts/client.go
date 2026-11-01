package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultBaseURL = "https://id.tainer.dev"

// Client talks to the tainer accounts service.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient returns a client for TAINER_ID_URL, or id.tainer.dev when unset.
func NewClient() *Client {
	base := os.Getenv("TAINER_ID_URL")
	if base == "" {
		base = defaultBaseURL
	}
	return &Client{BaseURL: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// TokenPair is the result of a login or refresh.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func (tp *TokenPair) validate() error {
	if tp.AccessToken == "" || tp.RefreshToken == "" {
		return errors.New("accounts service returned an incomplete token pair")
	}
	return nil
}

// User is the account returned by /v1/me.
type User struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// APIError is any non-2xx answer from the accounts service.
type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("accounts service returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("accounts service: %s (HTTP %d)", e.Code, e.Status)
}

// Login exchanges credentials for a token pair.
func (c *Client) Login(email, password, deviceName string) (*TokenPair, error) {
	var tp TokenPair
	err := c.do("POST", "/v1/login", "", map[string]string{"email": email, "password": password, "device_name": deviceName}, &tp)
	if err != nil {
		return nil, err
	}
	if err := tp.validate(); err != nil {
		return nil, err
	}
	return &tp, nil
}

// Refresh rotates the refresh token, returning a new pair.
func (c *Client) Refresh(refreshToken string) (*TokenPair, error) {
	var tp TokenPair
	if err := c.do("POST", "/v1/token", "", map[string]string{"refresh_token": refreshToken}, &tp); err != nil {
		return nil, err
	}
	if err := tp.validate(); err != nil {
		return nil, err
	}
	return &tp, nil
}

// Me returns the account behind an access token.
func (c *Client) Me(accessToken string) (*User, error) {
	var u User
	if err := c.do("GET", "/v1/me", accessToken, nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) do(method, path, bearer string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.BaseURL, "/")+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &env)
		return &APIError{Status: resp.StatusCode, Code: env.Error.Code}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}
