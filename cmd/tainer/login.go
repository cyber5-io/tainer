package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/cyber5-io/tainer/pkg/tainer/accounts"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// accountsDir is the directory holding the signed-in account (account.json):
// ~/.cyberstack of the invoking user, or TAINER_ACCOUNTS_DIR when set.
func accountsDir() string {
	if dir := os.Getenv("TAINER_ACCOUNTS_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cyberstack")
}

func cmdLogin(args []string) {
	exitFn(runLogin(args, os.Stdin, os.Stdout, accountsDir(), accounts.NewClient()))
}

func cmdLogout(args []string) {
	exitFn(runLogout(args, os.Stdout, accountsDir(), accounts.NewClient()))
}

func cmdWhoami(args []string) {
	exitFn(runWhoami(args, os.Stdout, accountsDir(), accounts.NewClient()))
}

// accountFailure converts an accounts client error into an envelope slug and a
// human message.
func accountFailure(err error) (slug, msg string) {
	var ae *accounts.APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case "invalid_credentials":
			return "login-failed", "incorrect email or password"
		case "email_unverified":
			return "email-unverified", "verify your email address before signing in"
		case "too_many_attempts":
			return "too-many-attempts", "too many attempts, try again later"
		}
		if ae.Status < 500 {
			return "command-failed", err.Error()
		}
	}
	return "id-unreachable", "could not reach the tainer accounts service"
}

// accountFailTo reports err in the mode in use and returns the exit status.
func accountFailTo(out io.Writer, asJSON bool, slug, msg string) int {
	if asJSON {
		return failTo(out, slug, msg)
	}
	fmt.Fprintln(os.Stderr, "tainer:", msg)
	return 1
}

type loginInput struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceName string `json:"deviceName"`
}

// runLogin implements `tainer login`. Under --json it reads one JSON object
// from stdin; otherwise it prompts. It returns the process exit code.
func runLogin(args []string, stdin io.Reader, out io.Writer, dir string, c *accounts.Client) int {
	flags, rest := tui.ParseOutputFlags(args)
	asJSON := flags.JSON
	if asJSON {
		jsonActive = true
	}
	if len(rest) > 0 {
		return accountFailTo(out, asJSON, "invalid-arguments", "usage: tainer login")
	}

	var in loginInput
	if asJSON {
		dec := json.NewDecoder(io.LimitReader(stdin, 1<<16))
		if err := dec.Decode(&in); err != nil {
			return failTo(out, "invalid-arguments", `stdin must be one JSON object {"email","password","deviceName"}`)
		}
	} else {
		var err error
		if in, err = promptLogin(stdin); err != nil {
			return accountFailTo(out, false, "invalid-arguments", err.Error())
		}
	}
	in.Email = strings.TrimSpace(in.Email)
	if in.Email == "" || in.Password == "" {
		return accountFailTo(out, asJSON, "invalid-arguments", "email and password are required")
	}
	if in.DeviceName == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "unknown"
		}
		in.DeviceName = host + " · tainer"
	}

	tp, err := c.Login(in.Email, in.Password, in.DeviceName)
	if err != nil {
		slug, msg := accountFailure(err)
		return accountFailTo(out, asJSON, slug, msg)
	}
	user, err := c.Me(tp.AccessToken)
	if err != nil {
		slug, msg := accountFailure(err)
		return accountFailTo(out, asJSON, slug, msg)
	}
	sess := &accounts.Session{
		Email:                user.Email,
		UserID:               user.ID,
		RefreshToken:         tp.RefreshToken,
		AccessToken:          tp.AccessToken,
		AccessTokenExpiresAt: time.Now().Add(time.Duration(tp.ExpiresIn) * time.Second),
	}
	if err := accounts.Save(dir, sess); err != nil {
		return accountFailTo(out, asJSON, "command-failed", "could not save the session: "+err.Error())
	}
	if !asJSON {
		fmt.Fprintf(out, "Logged in as %s\n", user.Email)
		return 0
	}
	return writeAccountDoc(out, struct {
		LoggedIn bool   `json:"loggedIn"`
		UserID   string `json:"userId"`
		Email    string `json:"email"`
	}{true, user.ID, user.Email})
}

// promptLogin asks for the email and password on the terminal. The password is
// read without echo when stdin is a terminal; piped input is read line by line.
func promptLogin(stdin io.Reader) (loginInput, error) {
	var in loginInput
	rd := bufio.NewReader(stdin)
	fmt.Fprint(os.Stderr, "Email: ")
	line, err := rd.ReadString('\n')
	if err != nil && line == "" {
		return in, errors.New("no email given")
	}
	in.Email = strings.TrimSpace(line)

	fmt.Fprint(os.Stderr, "Password: ")
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		pw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return in, fmt.Errorf("read password: %w", err)
		}
		in.Password = string(pw)
		return in, nil
	}
	pw, _ := rd.ReadString('\n')
	in.Password = strings.TrimRight(pw, "\r\n")
	return in, nil
}

// runWhoami implements `tainer whoami`: the signed-in account and a usable
// access token, refreshed on demand. Being signed out is not an error.
func runWhoami(args []string, out io.Writer, dir string, c *accounts.Client) int {
	flags, _ := tui.ParseOutputFlags(args)
	asJSON := flags.JSON
	if asJSON {
		jsonActive = true
	}
	loggedOut := func() int {
		if !asJSON {
			fmt.Fprintln(out, "Not logged in")
			return 0
		}
		return writeAccountDoc(out, struct {
			LoggedIn bool `json:"loggedIn"`
		}{false})
	}

	sess, err := accounts.Ensure(dir, c)
	offline := false
	if err != nil {
		var ae *accounts.APIError
		switch {
		case errors.As(err, &ae) && (ae.Code == "session_expired" || ae.Code == "session_revoked" || ae.Code == "invalid_token"):
			_ = accounts.Clear(dir)
			return loggedOut()
		case isUnreachable(err):
			stored, lerr := accounts.Load(dir)
			if lerr != nil || stored == nil || !time.Now().Before(stored.AccessTokenExpiresAt) {
				return accountFailTo(out, asJSON, "id-unreachable", "could not reach the tainer accounts service")
			}
			sess, offline = stored, true
		default:
			slug, msg := accountFailure(err)
			return accountFailTo(out, asJSON, slug, msg)
		}
	}
	if sess == nil {
		return loggedOut()
	}
	if !asJSON {
		fmt.Fprintln(out, sess.Email)
		return 0
	}
	doc := struct {
		LoggedIn             bool   `json:"loggedIn"`
		UserID               string `json:"userId"`
		Email                string `json:"email"`
		AccessToken          string `json:"accessToken"`
		AccessTokenExpiresAt string `json:"accessTokenExpiresAt"`
		Offline              bool   `json:"offline,omitempty"`
	}{true, sess.UserID, sess.Email, sess.AccessToken, jsonTime(sess.AccessTokenExpiresAt), offline}
	return writeAccountDoc(out, doc)
}

// isUnreachable reports a transport-level failure or an id-service 5xx; any
// other error from the accounts client is an answer from the service.
func isUnreachable(err error) bool {
	var ae *accounts.APIError
	if errors.As(err, &ae) {
		return ae.Status >= 500
	}
	return true
}

// runLogout implements `tainer logout`: best-effort server-side revoke, then
// the local session is always removed.
func runLogout(args []string, out io.Writer, dir string, c *accounts.Client) int {
	flags, _ := tui.ParseOutputFlags(args)
	asJSON := flags.JSON
	if asJSON {
		jsonActive = true
	}
	if sess, err := accounts.Load(dir); err == nil && sess != nil {
		_ = c.Logout(sess.RefreshToken)
	}
	if err := accounts.Clear(dir); err != nil {
		return accountFailTo(out, asJSON, "command-failed", "could not remove the session: "+err.Error())
	}
	if !asJSON {
		fmt.Fprintln(out, "Logged out")
		return 0
	}
	return writeAccountDoc(out, struct {
		LoggedOut bool `json:"loggedOut"`
	}{true})
}

func writeAccountDoc(out io.Writer, payload any) int {
	if err := writeJSONDoc(out, payload, true); err != nil {
		return failTo(out, "internal-error", err.Error())
	}
	return 0
}
