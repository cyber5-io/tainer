package main

import (
	"fmt"
	"io"
	"os"

	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

type apiCommand struct {
	Name   string `json:"name"`
	JSON   bool   `json:"json"`
	Stream bool   `json:"stream"`
}

// apiCommands lists every command that accepts --json, in a stable order.
// stream marks commands whose output is NDJSON rather than one document. The
// set matches the control gateway's allowlist; see docs/api/cli-json-v1.md.
var apiCommands = []apiCommand{
	{"status", true, false},
	{"list", true, false},
	{"init", true, false},
	{"start", true, false},
	{"stop", true, false},
	{"restart", true, false},
	{"destroy", true, false},
	{"db", true, false},
	{"update", true, false},
	{"network", true, false},
	{"doctor", true, false},
	{"logs", true, true},
	{"exec", true, true},
	{"config", true, false},
	{"api", true, false},
	{"watch", true, true},
	{"login", true, false},
	{"logout", true, false},
	{"whoami", true, false},
}

// cmdAPI implements `tainer api --json`: the machine-readable command index.
func cmdAPI(args []string) {
	exitFn(runAPI(args, os.Stdout, os.Stderr))
}

// runAPI is cmdAPI with its I/O injected; it returns the process exit code.
func runAPI(args []string, out, errOut io.Writer) int {
	flags, _ := tui.ParseOutputFlags(args)
	if !flags.JSON {
		fmt.Fprintln(errOut, "tainer api requires --json")
		return 1
	}
	jsonActive = true
	if err := jsonLine(out, struct {
		Commands []apiCommand `json:"commands"`
	}{apiCommands}); err != nil {
		return failTo(out, "internal-error", err.Error())
	}
	return 0
}
