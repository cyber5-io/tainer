package pod

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// DBExport dumps the pod's database to outPath. Uses mariadb-dump for
// MariaDB pods and pg_dump for Postgres pods (Kompozi or any pod with
// runtime.database=postgres).
func DBExport(ctx context.Context, eng *engine.Client, podName string, m *manifest.Manifest, outPath string) error {
	cmd := dumpCmd(m)
	res, err := eng.Exec(ctx, ContainerName(podName, RoleDB), cmd)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("dump exited %d: %s", res.ExitCode, res.Stderr)
	}
	return os.WriteFile(outPath, res.Stdout, 0644)
}

// DBImport reads a dump from inPath and pipes it into the pod's
// database container's restore command.
func DBImport(ctx context.Context, eng *engine.Client, podName string, m *manifest.Manifest, inPath string) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer in.Close()
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	cmd := restoreCmd(m)
	res, err := eng.ExecWithStdin(ctx, ContainerName(podName, RoleDB), cmd, data)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("restore exited %d: %s", res.ExitCode, res.Stderr)
	}
	return nil
}

func dumpCmd(m *manifest.Manifest) []string {
	if m.Runtime.Database == manifest.DatabasePostgres {
		return []string{"pg_dump", "-U", "tainer", m.Project.Name}
	}
	return []string{"mariadb-dump", "-u", "tainer", "-ptainer", m.Project.Name}
}

func restoreCmd(m *manifest.Manifest) []string {
	if m.Runtime.Database == manifest.DatabasePostgres {
		return []string{"psql", "-U", "tainer", m.Project.Name}
	}
	return []string{"mariadb", "-u", "tainer", "-ptainer", m.Project.Name}
}
