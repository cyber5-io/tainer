// Package main is the entry point for the tainer CLI — the user-facing
// tool that drives a CyberStack engine to run local development
// containers.
//
// Commands wired here:
//
//	tainer version                           print the linker-stamped version
//	tainer status                            probe cyberstackd, list pods
//	tainer init                              scaffold wizard (Task 24 placeholder)
//	tainer start [project]                   bring a pod up
//	tainer stop [project]                    stop a pod
//	tainer destroy [project] [--clean|--nuke] tear down a pod
//	tainer exec <project> [<role>] -- <cmd>  run a command in a container
//	tainer list (ls)                         list all pods
//	tainer db export|import <project> ...    dump / restore database
//	tainer update [--base <type>|--all|project] refresh images
//	tainer network mode show|set <mode>      inspect / switch network mode
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/initcmd"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/networkcmd"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
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
		cmdStatus()
	case "init":
		cmdInit(os.Args[2:])
	case "start":
		cmdStart(os.Args[2:])
	case "stop":
		cmdStop(os.Args[2:])
	case "destroy":
		cmdDestroy(os.Args[2:])
	case "exec":
		cmdExec(os.Args[2:])
	case "list", "ls":
		cmdList()
	case "db":
		cmdDB(os.Args[2:])
	case "update":
		cmdUpdate(os.Args[2:])
	case "network":
		cmdNetwork(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "tainer "+version)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "usage: tainer <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  init <type> [name]                     scaffold a new project into cwd")
	fmt.Fprintln(os.Stderr, "  start [project]                        start a project")
	fmt.Fprintln(os.Stderr, "  stop [project]                         stop a project")
	fmt.Fprintln(os.Stderr, "  destroy [project] [--clean|--nuke]     tear down a project")
	fmt.Fprintln(os.Stderr, "  exec <project> [<role>] -- <cmd...>    run a command in a container")
	fmt.Fprintln(os.Stderr, "  status                                 show pod state(s)")
	fmt.Fprintln(os.Stderr, "  list (ls)                              list all pods")
	fmt.Fprintln(os.Stderr, "  db export <project> [outfile]          dump database")
	fmt.Fprintln(os.Stderr, "  db import <project> <file>             restore database")
	fmt.Fprintln(os.Stderr, "  update [project|--base <type>|--all]   refresh image(s)")
	fmt.Fprintln(os.Stderr, "  network mode show|set <perf|compat>    inspect / switch network mode")
	fmt.Fprintln(os.Stderr, "  version                                print version")
}

// must prints err to stderr and exits 1. No-op when err is nil.
func must(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "tainer:", err)
	os.Exit(1)
}

// ctxWithTimeout is a convenience wrapper around context.WithTimeout.
func ctxWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// locateManifest resolves (manifestPath, projectDir) from args.
//
// With no args: cwd + tainer.yaml.
// With one arg: treated as a project name; resolved to ~/projects/<name>
// following the 0.2.x convention (a project registry lookup may be added
// later without changing call sites).
func locateManifest(args []string) (manifestPath, projectDir string) {
	if len(args) > 0 && args[0] != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			must(err)
		}
		projectDir = filepath.Join(home, "projects", args[0])
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			must(err)
		}
		projectDir = cwd
	}
	manifestPath = filepath.Join(projectDir, manifest.FileName)
	return manifestPath, projectDir
}

// cmdStatus probes cyberstackd and prints pod states.
func cmdStatus() {
	ctx, cancel := ctxWithTimeout(2 * time.Second)
	defer cancel()

	st := runtime.CurrentStatus(ctx, runtime.Options{})
	fmt.Printf("socket:    %s\n", st.Socket)
	fmt.Printf("running:   %v", st.Running)
	if st.PID > 0 {
		fmt.Printf(" (pid %d)", st.PID)
	}
	fmt.Println()
	fmt.Printf("reachable: %v\n", st.Reachable)

	if st.Reachable {
		eng, err := engine.New()
		if err != nil {
			return
		}
		defer eng.Close()
		pods, err := pod.List(ctx, eng)
		if err != nil {
			return
		}
		for _, p := range pods {
			// Locate manifest via label to get domain + ports.
			manifestPath := ""
			for _, c := range p.Containers {
				insp, err := eng.Inspect(ctx, c.Name)
				if err == nil && insp.Config != nil {
					manifestPath = insp.Config.Labels[pod.LabelManifestPath]
					break
				}
			}
			if manifestPath == "" {
				fmt.Printf("\n%s   pod %d   %s\n", p.Name, p.PodID, p.State())
				continue
			}
			m, err := manifest.Load(manifestPath)
			if err != nil {
				continue
			}
			fmt.Println()
			fmt.Print(pod.FormatStatus(p, m.Project.Domain, m.Ports))
		}
	}

	if !st.Reachable {
		os.Exit(1)
	}
}

// cmdInit scaffolds a new tainer project into the current working directory.
//
// Usage:
//
//	tainer init <type>         project name defaults to cwd basename
//	tainer init <type> <name>  explicit project name
//
// Matches legacy tainer 0.2.x: init operates on cwd, no project subdir.
func cmdInit(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: tainer init <type> [name]")
		fmt.Fprintln(os.Stderr, "  type: wordpress, php, nodejs, nextjs, nuxtjs, nestjs, react, kompozi")
		os.Exit(2)
	}
	opts := initcmd.Options{
		Type: manifest.ProjectType(args[0]),
	}
	if len(args) >= 2 {
		opts.Name = args[1]
	}
	// Name defaults to cwd basename inside initcmd.Run when opts.Name is empty.
	must(initcmd.Run(opts))
	fmt.Println("Next: tainer start")
}

// cmdStart locates the manifest (cwd or ~/projects/<name>), starts
// cyberstackd if needed, and calls pod.Start.
func cmdStart(args []string) {
	ctx, cancel := ctxWithTimeout(2 * time.Minute)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	must(err)
	defer eng.Close()

	manifestPath, projectDir := locateManifest(args)
	res, err := pod.Start(ctx, eng, pod.StartOptions{
		ManifestPath: manifestPath,
		ProjectDir:   projectDir,
	})
	must(err)

	fmt.Printf("%s started   (pod %d)\n", res.Pod, res.PodID)
	fmt.Printf("  https://%s\n", res.Domain)
	for _, h := range res.HTTPServices {
		fmt.Printf("  %s   # %s\n", h.URL, h.Role)
	}
	for _, t := range res.TCPServices {
		fmt.Printf("  %s   # %s\n", t.Host, t.Role)
	}
	fmt.Printf("  ssh %s@ssh.tainer.me\n", res.Pod)
}

// cmdStop stops a running pod. The pod name is derived from the manifest.
func cmdStop(args []string) {
	ctx, cancel := ctxWithTimeout(30 * time.Second)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	must(err)
	defer eng.Close()

	manifestPath, _ := locateManifest(args)
	m, err := manifest.Load(manifestPath)
	must(err)

	must(pod.Stop(ctx, eng, m.Project.Name))
	fmt.Printf("stopped %s\n", m.Project.Name)
}

// cmdDestroy tears down a pod. Flags: --clean, --nuke.
func cmdDestroy(args []string) {
	fs := flag.NewFlagSet("destroy", flag.ExitOnError)
	doClean := fs.Bool("clean", false, "remove tainer-managed files from the project dir")
	doNuke := fs.Bool("nuke", false, "also remove data/ (requires confirmation)")
	_ = fs.Parse(args)

	mode := pod.DestroyDefault
	if *doClean {
		mode = pod.DestroyClean
	}
	if *doNuke {
		mode = pod.DestroyNuke
	}

	ctx, cancel := ctxWithTimeout(30 * time.Second)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	must(err)
	defer eng.Close()

	manifestPath, projectDir := locateManifest(fs.Args())
	m, err := manifest.Load(manifestPath)
	must(err)

	prompt := func() string {
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		return line
	}

	must(pod.Destroy(ctx, eng, m.Project.Name, projectDir, mode, prompt))
	fmt.Printf("destroyed %s\n", m.Project.Name)
}

// cmdExec runs a command inside a pod container.
//
// Usage:
//
//	tainer exec <project> [<role>] -- <cmd...>
//	tainer exec <project> <cmd...>        (role resolved via ResolveExecRole)
func cmdExec(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: tainer exec <project> [<role>] -- <cmd...>")
		os.Exit(2)
	}

	projectName := args[0]
	rest := args[1:]

	// Look for "--" sentinel to separate role (optional) from command.
	sentinelIdx := -1
	for i, a := range rest {
		if a == "--" {
			sentinelIdx = i
			break
		}
	}

	var argRole string
	var cmd []string

	if sentinelIdx >= 0 {
		// Everything before "--" is the optional role; everything after is cmd.
		if sentinelIdx > 0 {
			argRole = rest[0]
		}
		cmd = rest[sentinelIdx+1:]
	} else {
		// No "--": all remaining args are the command; role defaults.
		cmd = rest
	}

	if len(cmd) == 0 {
		fmt.Fprintln(os.Stderr, "tainer: exec requires a command")
		os.Exit(2)
	}

	// Locate manifest to resolve the project type for ResolveExecRole.
	manifestPath, _ := locateManifest([]string{projectName})
	m, err := manifest.Load(manifestPath)
	must(err)

	role := pod.ResolveExecRole(m.Project.Type, argRole)

	ctx, cancel := ctxWithTimeout(5 * time.Minute)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	must(err)
	defer eng.Close()

	code, err := pod.Exec(ctx, eng, projectName, role, cmd)
	must(err)
	os.Exit(code)
}

// cmdList lists all pods known to the engine.
func cmdList() {
	ctx, cancel := ctxWithTimeout(10 * time.Second)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	must(err)
	defer eng.Close()

	pods, err := pod.List(ctx, eng)
	must(err)

	for _, p := range pods {
		fmt.Printf("%s\t%s\tpod %d\n", p.Name, p.State(), p.PodID)
	}
}

// cmdDB handles `tainer db export` and `tainer db import`.
func cmdDB(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tainer db export <project> [outfile]")
		fmt.Fprintln(os.Stderr, "       tainer db import <project> <infile>")
		os.Exit(2)
	}

	sub := args[0]
	switch sub {
	case "export":
		cmdDBExport(args[1:])
	case "import":
		cmdDBImport(args[1:])
	default:
		fmt.Fprintln(os.Stderr, "tainer: unknown db subcommand:", sub)
		os.Exit(2)
	}
}

func cmdDBExport(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: tainer db export <project> [outfile]")
		os.Exit(2)
	}

	projectName := args[0]
	manifestPath, _ := locateManifest([]string{projectName})
	m, err := manifest.Load(manifestPath)
	must(err)

	outPath := ""
	if len(args) >= 2 {
		outPath = args[1]
	} else {
		outPath = fmt.Sprintf("%s-%s.sql", projectName, time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	}

	ctx, cancel := ctxWithTimeout(5 * time.Minute)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	must(err)
	defer eng.Close()

	must(pod.DBExport(ctx, eng, projectName, m, outPath))
	fmt.Printf("exported %s → %s\n", projectName, outPath)
}

func cmdDBImport(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tainer db import <project> <infile>")
		os.Exit(2)
	}

	projectName := args[0]
	inPath := args[1]

	manifestPath, _ := locateManifest([]string{projectName})
	m, err := manifest.Load(manifestPath)
	must(err)

	ctx, cancel := ctxWithTimeout(5 * time.Minute)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	must(err)
	defer eng.Close()

	must(pod.DBImport(ctx, eng, projectName, m, inPath))
	fmt.Printf("imported %s → %s\n", inPath, projectName)
}

// cmdUpdate refreshes images for one or all pods.
//
//	--base <type>  pull base images for <type> only (no pod changes)
//	--all          iterate all known pods and update each
//	[project]      update a single named project (cwd if omitted)
func cmdUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	baseType := fs.String("base", "", "refresh base image for project type")
	all := fs.Bool("all", false, "update all pods")
	_ = fs.Parse(args)

	ctx, cancel := ctxWithTimeout(10 * time.Minute)
	defer cancel()

	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: false})
	must(err)
	defer eng.Close()

	if *baseType != "" {
		must(pod.Update(ctx, eng, pod.StartOptions{}, pod.UpdateOptions{
			BaseOnly: true,
			BaseType: manifest.ProjectType(*baseType),
		}))
		fmt.Printf("updated base image for %s\n", *baseType)
		return
	}

	if *all {
		pods, err := pod.List(ctx, eng)
		must(err)
		for _, p := range pods {
			// Recover manifest path from label.
			manifestPath := ""
			for _, c := range p.Containers {
				insp, err := eng.Inspect(ctx, c.Name)
				if err == nil && insp.Config != nil {
					manifestPath = insp.Config.Labels[pod.LabelManifestPath]
					break
				}
			}
			if manifestPath == "" {
				fmt.Fprintf(os.Stderr, "warning: skipping %s (manifest path unknown)\n", p.Name)
				continue
			}
			projectDir := filepath.Dir(manifestPath)
			if err := pod.Update(ctx, eng, pod.StartOptions{ManifestPath: manifestPath, ProjectDir: projectDir}, pod.UpdateOptions{}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: update %s: %v\n", p.Name, err)
			} else {
				fmt.Printf("updated %s\n", p.Name)
			}
		}
		return
	}

	// Single project (cwd or named arg).
	manifestPath, projectDir := locateManifest(fs.Args())
	must(pod.Update(ctx, eng, pod.StartOptions{ManifestPath: manifestPath, ProjectDir: projectDir}, pod.UpdateOptions{}))
	m, err := manifest.Load(manifestPath)
	must(err)
	fmt.Printf("updated %s\n", m.Project.Name)
}

// cmdNetwork handles `tainer network mode show|set <perf|compat>`.
func cmdNetwork(args []string) {
	if len(args) < 2 || args[0] != "mode" {
		fmt.Fprintln(os.Stderr, "usage: tainer network mode show|set <perf|compat>")
		os.Exit(2)
	}
	switch args[1] {
	case "show":
		must(networkcmd.Show())
	case "set":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: tainer network mode set <perf|compat>")
			os.Exit(2)
		}
		ctx, cancel := ctxWithTimeout(60 * time.Second)
		defer cancel()
		must(networkcmd.Set(ctx, args[2]))
	default:
		fmt.Fprintln(os.Stderr, "tainer: unknown network subcommand:", args[1])
		os.Exit(2)
	}
}
