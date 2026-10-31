package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/docker/docker/pkg/stdcopy"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// splitProjectRole resolves the positional arguments shared by commands that
// take `[project] [role]`: a lone positional is the project when it is
// registered, otherwise the role.
func splitProjectRole(pos []string) (project, role string) {
	switch len(pos) {
	case 0:
	case 1:
		if isRegisteredProject(pos[0]) {
			project = pos[0]
		} else {
			role = pos[0]
		}
	default:
		project, role = pos[0], pos[1]
	}
	return project, role
}

func loadManifestFor(project string) (*manifest.Manifest, string) {
	var locator []string
	if project != "" {
		locator = []string{project}
	}
	path, _, err := locateManifest(locator)
	must(err)
	m, err := manifest.Load(path)
	must(withCode("manifest-invalid", err))
	return m, path
}

// cmdLogs implements `tainer logs [project] [role] [--follow]`.
//
// Without a role it streams every container of the pod. Output flags:
// --plain (default), --json (NDJSON, one record per log line).
func cmdLogs(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := resolveMode(flags, false)

	follow := false
	var pos []string
	for _, a := range rest {
		switch {
		case a == "--follow" || a == "-f":
			follow = true
		case strings.HasPrefix(a, "-"):
			must(withCode("invalid-arguments", fmt.Errorf("unknown flag %s (usage: tainer logs [project] [role] [--follow])", a)))
		default:
			pos = append(pos, a)
		}
	}
	project, role := splitProjectRole(pos)
	m, _ := loadManifestFor(project)
	roles := pod.RolesForPod(m)
	if role != "" {
		found := false
		for _, r := range roles {
			found = found || r == role
		}
		if !found {
			must(withCode("role-not-found", fmt.Errorf("%s has no %q container (roles: %s)", m.Project.Name, role, strings.Join(roles, ", "))))
		}
		roles = []string{role}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(engineErr(err))
	defer eng.Close()

	out := &lockedWriter{w: os.Stdout}
	var wg sync.WaitGroup
	errs := make([]error, len(roles))
	for i, r := range roles {
		wg.Add(1)
		go func(i int, part string) {
			defer wg.Done()
			cname := pod.ContainerName(m.Project.Name, part)
			insp, err := eng.Inspect(ctx, cname)
			if err != nil {
				errs[i] = withCode("logs-unavailable", fmt.Errorf("%s: %w", part, err))
				return
			}
			rc, err := eng.Logs(ctx, cname, follow)
			if err != nil {
				errs[i] = withCode("logs-unavailable", fmt.Errorf("%s: %w", part, err))
				return
			}
			defer rc.Close()

			pr, pw := io.Pipe()
			go func() {
				var cerr error
				if insp.Config != nil && insp.Config.Tty {
					_, cerr = io.Copy(pw, rc)
				} else {
					_, cerr = stdcopy.StdCopy(pw, pw, rc)
				}
				_ = pw.CloseWithError(cerr)
			}()
			if mode == tui.ModeJSON {
				errs[i] = streamLogLines(pr, out, m.Project.Name, part)
			} else {
				errs[i] = streamLogLinesPlain(pr, out, part)
			}
		}(i, r)
	}
	wg.Wait()
	for _, e := range errs {
		must(e)
	}
}

// streamLogLinesPlain writes `part | line` for each line of r.
func streamLogLinesPlain(r io.Reader, w io.Writer, part string) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if _, err := fmt.Fprintf(w, "%s | %s\n", part, sc.Text()); err != nil {
			return err
		}
	}
	return sc.Err()
}

// cmdConfig implements `tainer config [project]`: print the project's manifest.
// Human modes print tainer.yaml as written; --json prints the parsed manifest.
func cmdConfig(args []string) {
	flags, rest := tui.ParseOutputFlags(args)
	mode := resolveMode(flags, false)
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			must(withCode("invalid-arguments", fmt.Errorf("unknown flag %s (usage: tainer config [project])", a)))
		}
	}
	var project string
	if len(rest) > 0 {
		project = rest[0]
	}
	m, path := loadManifestFor(project)
	if mode == tui.ModeJSON {
		must(withCode("internal-error", emitConfigJSON(m)))
		return
	}
	raw, err := os.ReadFile(path)
	must(withCode("manifest-invalid", err))
	_, _ = os.Stdout.Write(raw)
}
