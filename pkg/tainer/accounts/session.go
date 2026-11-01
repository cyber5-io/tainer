package accounts

import (
	"errors"
	"time"
)

// accessTokenSkew is how much lifetime an access token must have left to be
// reused without refreshing.
const accessTokenSkew = 60 * time.Second

// Ensure returns a session whose access token is usable for at least the next
// minute, refreshing (and persisting the rotated pair) when needed. It returns
// (nil, nil) when nobody is logged in.
//
// Every refresh invalidates the submitted refresh token, so the rotated pair is
// saved before Ensure returns. If the server rejects the token as invalid,
// another local process may have rotated it first; the file is re-read once and
// the refresh retried from whatever it now holds.
func Ensure(dir string, c *Client) (*Session, error) {
	s, err := Load(dir)
	if err != nil || s == nil {
		return nil, err
	}
	if time.Until(s.AccessTokenExpiresAt) > accessTokenSkew {
		return s, nil
	}
	tp, err := c.Refresh(s.RefreshToken)
	var ae *APIError
	if errors.As(err, &ae) && ae.Code == "invalid_token" {
		latest, lerr := Load(dir)
		if lerr != nil || latest == nil {
			return nil, lerr
		}
		if latest.RefreshToken == s.RefreshToken {
			return nil, err
		}
		if time.Until(latest.AccessTokenExpiresAt) > accessTokenSkew {
			return latest, nil
		}
		s = latest
		tp, err = c.Refresh(s.RefreshToken)
	}
	if err != nil {
		return nil, err
	}
	s.AccessToken = tp.AccessToken
	s.RefreshToken = tp.RefreshToken
	s.AccessTokenExpiresAt = time.Now().Add(time.Duration(tp.ExpiresIn) * time.Second)
	if err := Save(dir, s); err != nil {
		return nil, err
	}
	return s, nil
}
