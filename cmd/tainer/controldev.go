package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/cyber5-io/tainer/pkg/tainer"
	"github.com/cyber5-io/tainer/pkg/tainer/controldev"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// controlDevWarning is shown, with the folder, before every approval.
const controlDevWarning = "This project will be able to run any tainer command on this machine, including deleting pods and reading their databases."

const controlDevUsage = `usage:
  tainer control dev allow [folder]    let a dashboard checkout use the control pod's access
  tainer control dev list              show approved folders
  tainer control dev revoke [folder]   take the access away again
The folder defaults to the current one.`

// controlDevApplyBudget bounds bringing the affected pods in line.
const controlDevApplyBudget = 5 * time.Minute

// controlDevOps is everything `tainer control dev` touches besides the
// approvals file, so tests can drive it without a terminal or a runtime.
type controlDevOps struct {
	stdin      io.Reader
	isTerminal func() bool
	cwd        func() (string, error)
	// apply brings the dashboard checkout at dir and the control pod in
	// line with the approvals right away. boot says whether tainer may be
	// started for it; otherwise a stopped tainer is left alone (every start
	// re-checks the approval).
	apply func(ctx context.Context, dir string, boot bool) error
}

func liveControlDevOps() controlDevOps {
	return controlDevOps{
		stdin:      os.Stdin,
		isTerminal: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		cwd:        os.Getwd,
		apply:      applyControlDev,
	}
}

// runControlDev implements `tainer control dev allow|list|revoke`. The trust
// boundary is the user's own terminal: approvals are recorded only from an
// interactive one, only for a folder carrying the control-dev marker, and
// only after the user has read what it grants.
func runControlDev(ctx context.Context, args []string, stdout, stderr io.Writer, ops controlDevOps) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, controlDevUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch {
	case sub == "list" && len(rest) == 0:
		paths, err := controldev.List()
		if err != nil {
			fmt.Fprintln(stderr, "tainer:", err)
			return 1
		}
		if len(paths) == 0 {
			fmt.Fprintln(stderr, "No folders are approved.")
		}
		for _, p := range paths {
			fmt.Fprintln(stdout, p)
		}
		return 0
	case (sub == "allow" || sub == "revoke") && len(rest) <= 1:
		dir, err := devFolder(rest, ops)
		if err != nil {
			fmt.Fprintln(stderr, "tainer:", err)
			return 1
		}
		if sub == "allow" {
			return controlDevAllow(ctx, dir, stdout, stderr, ops)
		}
		return controlDevRevoke(ctx, dir, stdout, stderr, ops)
	}
	fmt.Fprintln(stderr, controlDevUsage)
	return 2
}

// devFolder is the folder argument, or the current folder, canonicalised.
func devFolder(rest []string, ops controlDevOps) (string, error) {
	dir := ""
	if len(rest) == 1 {
		dir = rest[0]
	} else {
		d, err := ops.cwd()
		if err != nil {
			return "", fmt.Errorf("current folder: %w", err)
		}
		dir = d
	}
	return controldev.Canonical(dir)
}

func controlDevAllow(ctx context.Context, dir string, stdout, stderr io.Writer, ops controlDevOps) int {
	if !ops.isTerminal() {
		fmt.Fprintln(stderr, "tainer: approving a folder needs you at the keyboard — run this in a terminal")
		return 1
	}
	m, err := manifest.Load(filepath.Join(dir, manifest.FileName))
	if err != nil {
		fmt.Fprintf(stderr, "tainer: no tainer project in %s\n", dir)
		return 1
	}
	if !m.Project.ControlDev {
		fmt.Fprintf(stderr, "tainer: %s isn't a control dashboard checkout — its tainer.yaml needs `control-dev: true` under project:\n", dir)
		return 1
	}
	if m.Project.Type != manifest.TypeNodeJS {
		fmt.Fprintf(stderr, "tainer: only nodejs projects can be dashboard checkouts — %s is a %s project\n", dir, m.Project.Type)
		return 1
	}
	if tainer.IsPinnedProject(m.Project.Name) {
		fmt.Fprintf(stderr, "tainer: %q is the built-in dashboard's name — rename the project in %s first (e.g. control-dev)\n", m.Project.Name, filepath.Join(dir, manifest.FileName))
		return 1
	}

	fmt.Fprintf(stdout, "Folder: %s\n\n%s\n\nAllow? [y/N] ", dir, controlDevWarning)
	line, _ := bufio.NewReader(ops.stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		fmt.Fprintln(stdout, "Not approved.")
		return 1
	}
	if err := controldev.Allow(dir); err != nil {
		fmt.Fprintln(stderr, "tainer:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Approved. %s gets the control pod's access when it starts; `tainer control dev revoke` takes it away.\n", m.Project.Name)
	if err := ops.apply(ctx, dir, false); err != nil {
		fmt.Fprintf(stderr, "tainer: approved, but couldn't restart the affected pods yet; run `tainer restart %s`\n", m.Project.Name)
		return 1
	}
	return 0
}

func controlDevRevoke(ctx context.Context, dir string, stdout, stderr io.Writer, ops controlDevOps) int {
	if err := controldev.Revoke(dir); err != nil {
		if errors.Is(err, controldev.ErrNotApproved) {
			fmt.Fprintf(stderr, "tainer: %s isn't approved (see `tainer control dev list`)\n", dir)
		} else {
			fmt.Fprintln(stderr, "tainer:", err)
		}
		return 1
	}
	fmt.Fprintf(stdout, "Revoked: %s\n", dir)
	name := ""
	if m, err := manifest.Load(filepath.Join(dir, manifest.FileName)); err == nil {
		name = m.Project.Name
	}
	// Applied at once to a running tainer. A stopped one is left alone:
	// checkout containers with access never restart by themselves, and the
	// once-per-boot ensure removes any whose approval is gone.
	if err := ops.apply(ctx, dir, false); err != nil {
		if name != "" {
			fmt.Fprintf(stderr, "tainer: revoked, but couldn't apply it to the running pod — run `tainer stop %s` now\n", name)
		} else {
			fmt.Fprintf(stderr, "tainer: revoked, but couldn't apply it to the running pod — stop the project that was in %s now\n", dir)
		}
		return 1
	}
	return 0
}

// applyControlDev re-checks the approval on the checkout's pod and the
// control pod's approval count, now. With boot false and tainer not running
// there is nothing to do: every start re-checks.
func applyControlDev(ctx context.Context, dir string, boot bool) error {
	ctx, cancel := context.WithTimeout(ctx, controlDevApplyBudget)
	defer cancel()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: boot})
	if err != nil {
		if !boot {
			return nil
		}
		return err
	}
	defer eng.Close()

	var errs []error
	mf := filepath.Join(dir, manifest.FileName)
	if manifest.Exists(dir) {
		if err := pod.ReconcileControlAccess(ctx, eng, mf, dir); err != nil {
			errs = append(errs, err)
		}
	}
	release, err := waitControlLock(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	defer release()
	if err := ensureControlPod(ctx, eng); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
