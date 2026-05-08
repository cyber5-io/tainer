package pod

import (
	"context"
	"fmt"
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

// Exec runs cmd inside the pod's container of the resolved role.
// Streams stdout/stderr to the user's terminal and returns the exit
// code.
func Exec(ctx context.Context, eng *engine.Client, podName, role string, cmd []string) (int, error) {
	res, err := eng.Exec(ctx, ContainerName(podName, role), cmd)
	if err != nil {
		return 1, err
	}
	_, _ = os.Stdout.Write(res.Stdout)
	_, _ = os.Stderr.Write(res.Stderr)
	return res.ExitCode, nil
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
