package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/cyber5-io/tainer/pkg/tainer/accounts"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

func cmdSignup(args []string) {
	exitFn(runSignup(args, os.Stdin, os.Stdout, accounts.NewClient()))
}

func cmdPasswordReset(args []string) {
	exitFn(runPasswordReset(args, os.Stdout, accounts.NewClient()))
}

type signupInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// runSignup implements `tainer signup`. Under --json it reads one JSON object
// from stdin; otherwise it prompts. It never signs in and never touches the
// stored session: the account exists only once the emailed link is followed.
func runSignup(args []string, stdin io.Reader, out io.Writer, c *accounts.Client) int {
	flags, rest := tui.ParseOutputFlags(args)
	asJSON := flags.JSON
	if asJSON {
		jsonActive = true
	}
	if len(rest) > 1 {
		return accountFailTo(out, asJSON, "invalid-arguments", "usage: tainer signup [email]")
	}
	argEmail := ""
	if len(rest) == 1 {
		argEmail = strings.TrimSpace(rest[0])
	}

	var in signupInput
	if asJSON {
		dec := json.NewDecoder(io.LimitReader(stdin, 1<<16))
		if err := dec.Decode(&in); err != nil {
			return failTo(out, "invalid-arguments", `stdin must be one JSON object {"email","password"}`)
		}
	} else {
		var err error
		if in, err = promptSignup(stdin, argEmail); err != nil {
			return accountFailTo(out, false, "invalid-arguments", err.Error())
		}
	}
	in.Email = strings.TrimSpace(in.Email)
	if in.Email == "" {
		in.Email = argEmail
	}
	if in.Email == "" || in.Password == "" {
		return accountFailTo(out, asJSON, "invalid-arguments", "email and password are required")
	}

	if err := c.Signup(in.Email, in.Password); err != nil {
		slug, msg := emailFailure(err)
		return accountFailTo(out, asJSON, slug, msg)
	}
	if !asJSON {
		fmt.Fprintln(out, "Check your email to confirm your address.")
		return 0
	}
	return writeAccountDoc(out, struct {
		SignedUp bool `json:"signedUp"`
	}{true})
}

// emailFailure is accountFailure for the commands that take an email address:
// a 400 invalid_request there means the address itself was rejected.
func emailFailure(err error) (slug, msg string) {
	var ae *accounts.APIError
	if errors.As(err, &ae) && ae.Code == "invalid_request" {
		return "invalid-email", "enter a valid email address"
	}
	return accountFailure(err)
}

// promptSignup asks for the email (unless it was given as an argument), the
// password and its confirmation. The passwords are read without echo when
// stdin is a terminal.
func promptSignup(stdin io.Reader, email string) (signupInput, error) {
	var in signupInput
	rd := bufio.NewReader(stdin)
	if email != "" {
		in.Email = email
	} else {
		fmt.Fprint(os.Stderr, "Email: ")
		line, err := rd.ReadString('\n')
		if err != nil && line == "" {
			return in, errors.New("no email given")
		}
		in.Email = strings.TrimSpace(line)
	}

	pw, err := promptSecret(stdin, rd, "Password: ")
	if err != nil {
		return in, err
	}
	confirm, err := promptSecret(stdin, rd, "Confirm password: ")
	if err != nil {
		return in, err
	}
	if pw != confirm {
		return in, errors.New("passwords do not match")
	}
	in.Password = pw
	return in, nil
}

func promptSecret(stdin io.Reader, rd *bufio.Reader, label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		pw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(pw), nil
	}
	pw, _ := rd.ReadString('\n')
	return strings.TrimRight(pw, "\r\n"), nil
}

// runPasswordReset implements `tainer password-reset <email>`. The service
// answers the same whether or not the address has an account, and so do we.
func runPasswordReset(args []string, out io.Writer, c *accounts.Client) int {
	flags, rest := tui.ParseOutputFlags(args)
	asJSON := flags.JSON
	if asJSON {
		jsonActive = true
	}
	if len(rest) != 1 || strings.TrimSpace(rest[0]) == "" {
		return accountFailTo(out, asJSON, "invalid-arguments", "usage: tainer password-reset <email>")
	}
	if err := c.RequestPasswordReset(strings.TrimSpace(rest[0])); err != nil {
		slug, msg := emailFailure(err)
		return accountFailTo(out, asJSON, slug, msg)
	}
	if !asJSON {
		fmt.Fprintln(out, "If that address has an account, a reset link is on its way.")
		return 0
	}
	return writeAccountDoc(out, struct {
		Requested bool `json:"requested"`
	}{true})
}
