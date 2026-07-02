package pod

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// ResolveExecRole picks the role for `tainer exec` when no explicit
// role argument is given.
func ResolveExecRole(t manifest.ProjectType, argRole string) string {
	if argRole != "" {
		return argRole
	}
	return DefaultExecRole(t)
}

// ExecOptions configures an interactive exec run against a pod
// container. See the field docs and the equivalent flags on
// cmdExec for the user-facing shape.
type ExecOptions struct {
	Cmd     []string
	User    string
	WorkDir string
	Env     []string
	Tty     bool
	Stdin   io.Reader // nil to skip stdin attach
	Stdout  io.Writer // required
	Stderr  io.Writer // required

	// OnAttached: see engine.ExecStreamOptions.OnAttached (TTY resize
	// hook).
	OnAttached func(resize func(width, height uint16) error)
}

// Exec runs cmd inside the pod's container of the resolved role.
// Streams stdio live and returns the exit code.
//
// The old "buffer and dump" behaviour has been replaced with a
// streaming pump; internal callers that need the buffered variant
// should hit engine.Exec directly (see pod/db.go for the pattern).
func Exec(ctx context.Context, eng *engine.Client, podName, role string, opts ExecOptions) (int, error) {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	return eng.ExecStream(ctx, ContainerName(podName, role), engine.ExecStreamOptions{
		Cmd:        opts.Cmd,
		User:       opts.User,
		WorkDir:    opts.WorkDir,
		Env:        opts.Env,
		Tty:        opts.Tty,
		Stdin:      opts.Stdin,
		Stdout:     opts.Stdout,
		Stderr:     opts.Stderr,
		OnAttached: opts.OnAttached,
	})
}

// FormatStatus formats one Pod for the `tainer status` output. The
// pod line is highlighted because the spec promised it appears on
// every status print.
func FormatStatus(p Pod, domain string, ports []manifest.PortEntry) string {
	out := fmt.Sprintf("%s   pod %d   %s\n", p.Name, p.PodID, p.State())
	out += fmt.Sprintf("  https://%s\n", domain)
	for _, port := range ports {
		switch port.Protocol {
		case manifest.PortHTTP:
			out += fmt.Sprintf("  https://%s:%d   # %s\n", domain, port.Container, port.Role)
		case manifest.PortTCP:
			host := DerivePort(p.PodID, port.Role)
			out += fmt.Sprintf("  127.0.0.1:%d   # %s\n", host, port.Role)
		}
	}
	out += fmt.Sprintf("  ssh %s@ssh.tainer.me\n", p.Name)
	return out
}
