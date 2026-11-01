// Package accounts is the host-side client for the tainer accounts service
// and the on-disk session store the CLI shares across processes.
package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const sessionFile = "account.json"

// Session is the logged-in state persisted in <dir>/account.json.
type Session struct {
	Email                string    `json:"email"`
	UserID               string    `json:"user_id"`
	RefreshToken         string    `json:"refresh_token"`
	AccessToken          string    `json:"access_token"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
}

// Load reads the session from dir. It returns (nil, nil) when none is stored.
func Load(dir string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(dir, sessionFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", sessionFile, err)
	}
	return &s, nil
}

// Save writes the session atomically with mode 0600, creating dir if needed.
func Save(dir string, s *Session) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, sessionFile+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, sessionFile))
}

// Clear removes the stored session; it is a no-op when none exists.
func Clear(dir string) error {
	err := os.Remove(filepath.Join(dir, sessionFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
