// Package main is the entry point for the tainer CLI — the user-facing
// tool that drives a CyberStack engine to run local development
// containers.
//
// This is the dev/v1 rebuild stub. The full CLI surface (init, start,
// stop, list, exec, db, status, …) is being ported off the legacy podman
// fork (under legacy/cmd/podman/tainer/) and rewired through
// pkg/tainer/engine + pkg/tainer/runtime per
// docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md.
//
// Current commands:
//
//	tainer version  — print the linker-stamped version
//	tainer status   — probe cyberstackd, print Running/Reachable/Socket
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// version is stamped at link time; see Makefile.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "status":
		statusCmd()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "tainer "+version+" (rebuild on dev/v1)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "usage: tainer <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  version    print version")
	fmt.Fprintln(os.Stderr, "  status     show cyberstackd reachability")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "more commands land as the rebuild proceeds; see")
	fmt.Fprintln(os.Stderr, "docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md")
}

func statusCmd() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	st := runtime.CurrentStatus(ctx, runtime.Options{})

	fmt.Printf("socket:    %s\n", st.Socket)
	fmt.Printf("running:   %v", st.Running)
	if st.PID > 0 {
		fmt.Printf(" (pid %d)", st.PID)
	}
	fmt.Println()
	fmt.Printf("reachable: %v\n", st.Reachable)

	if !st.Reachable {
		os.Exit(1)
	}
}
