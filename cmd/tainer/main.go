// Package main is the entry point for the tainer CLI — the user-facing tool
// that drives a CyberStack engine to run local development containers.
//
// This is a stub. The full CLI is being ported from legacy/cmd/podman/tainer/
// per the rebuild plan in docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md.
package main

import (
	"fmt"
	"os"
)

// version is stamped at link time; see Makefile.
var version = "dev"

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	fmt.Fprintln(os.Stderr, "tainer: CLI under construction (rebuild on dev/v1)")
	fmt.Fprintln(os.Stderr, "        old tree at legacy/, see docs/superpowers/ for plan")
	os.Exit(1)
}
