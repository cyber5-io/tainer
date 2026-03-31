package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/containers/podman/v6/cmd/podman/artifact"
	_ "github.com/containers/podman/v6/cmd/podman/completion"
	_ "github.com/containers/podman/v6/cmd/podman/farm"
	_ "github.com/containers/podman/v6/cmd/podman/generate"
	_ "github.com/containers/podman/v6/cmd/podman/healthcheck"
	_ "github.com/containers/podman/v6/cmd/podman/images"
	_ "github.com/containers/podman/v6/cmd/podman/kube"
	_ "github.com/containers/podman/v6/cmd/podman/machine"
	_ "github.com/containers/podman/v6/cmd/podman/machine/os"
	_ "github.com/containers/podman/v6/cmd/podman/manifest"
	_ "github.com/containers/podman/v6/cmd/podman/networks"
	_ "github.com/containers/podman/v6/cmd/podman/pods"
	_ "github.com/containers/podman/v6/cmd/podman/quadlet"
	"github.com/containers/podman/v6/cmd/podman/registry"
	_ "github.com/containers/podman/v6/cmd/podman/secrets"
	_ "github.com/containers/podman/v6/cmd/podman/system"
	_ "github.com/containers/podman/v6/cmd/podman/system/connection"
	_ "github.com/containers/podman/v6/cmd/podman/tainer"
	"github.com/containers/podman/v6/cmd/podman/validate"
	_ "github.com/containers/podman/v6/cmd/podman/volumes"
	"github.com/containers/podman/v6/pkg/domain/entities"
	"github.com/containers/podman/v6/pkg/logiface"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tui/home"
	"github.com/containers/podman/v6/pkg/rootless"
	"github.com/containers/podman/v6/pkg/terminal"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/storage/pkg/reexec"
	"golang.org/x/term"
)

type logrusLogger struct{}

func (l logrusLogger) Errorf(format string, args ...any) {
	logrus.Errorf(format, args...)
}

func (l logrusLogger) Debugf(format string, args ...any) {
	logrus.Debugf(format, args...)
}

func main() {
	if reexec.Init() {
		// We were invoked with a different argv[0] indicating that we
		// had a specific job to do as a subprocess, and it's done.
		return
	}
	logiface.SetLogger(logrusLogger{})

	if filepath.Base(os.Args[0]) == registry.PodmanSh ||
		(len(os.Args[0]) > 0 && filepath.Base(os.Args[0][1:]) == registry.PodmanSh) {
		shell := strings.TrimPrefix(os.Args[0], "-")
		cfg := registry.PodmanConfig()

		args := []string{shell, "exec", "-i", "--wait", strconv.FormatUint(uint64(cfg.ContainersConfDefaultsRO.PodmanshTimeout()), 10)}
		if term.IsTerminal(0) || term.IsTerminal(1) || term.IsTerminal(2) {
			args = append(args, "-t")
		}
		args = append(args, cfg.ContainersConfDefaultsRO.Podmansh.Container, cfg.ContainersConfDefaultsRO.Podmansh.Shell)
		if len(os.Args) > 1 {
			args = append(args, os.Args[1:]...)
		}
		os.Args = args
	}

	rootCmd = parseCommands()

	Execute()
	os.Exit(0)
}

// tainerVisibleCommands lists the commands shown in default help.
// All other commands are hidden but still functional.
var tainerVisibleCommands = map[string]bool{
	"init":    true,
	"start":   true,
	"stop":    true,
	"restart": true,
	"destroy": true,
	"list":    true,
	"pods":    true,
	"status":  true,
	"exec":    true,
	"mount":   true,
	"node":    true,
	"yarn":    true,
	"npm":     true,
	"update":  true,
	"version": true,
	"machine": true,
	"help":    true,
}

func parseCommands() *cobra.Command {
	cfg := registry.PodmanConfig()
	for _, c := range registry.Commands {
		if supported, found := c.Command.Annotations[registry.EngineMode]; found {
			if cfg.EngineMode.String() != supported {
				var client string
				switch cfg.EngineMode {
				case entities.TunnelMode:
					client = "remote"
				case entities.ABIMode:
					client = "local"
				}

				// add error message to the command so the user knows that this command is not supported with local/remote
				c.Command.RunE = func(cmd *cobra.Command, _ []string) error {
					return fmt.Errorf("cannot use command %q with the %s tainer client", cmd.CommandPath(), client)
				}
				// turn off flag parsing to make we do not get flag errors
				c.Command.DisableFlagParsing = true

				// mark command as hidden so it is not shown in --help
				c.Command.Hidden = true

				// overwrite persistent pre/post function to skip setup
				c.Command.PersistentPostRunE = validate.NoOp
				c.Command.PersistentPreRunE = validate.NoOp
				addCommand(c)
				continue
			}
		}

		// Command cannot be run rootless
		_, found := c.Command.Annotations[registry.UnshareNSRequired]
		if found {
			if rootless.IsRootless() && os.Getuid() != 0 {
				c.Command.RunE = func(cmd *cobra.Command, _ []string) error {
					return fmt.Errorf("cannot run command %q in rootless mode, must execute `tainer unshare` first", cmd.CommandPath())
				}
			}
		}
		addCommand(c)
	}

	// Hide all commands not in the tainer visible set.
	// Track seen names to hide duplicates (e.g., podman's "update" vs ours).
	seen := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		if !tainerVisibleCommands[cmd.Name()] || seen[cmd.Name()] {
			cmd.Hidden = true
		}
		seen[cmd.Name()] = true
	}

	if err := terminal.SetConsole(); err != nil {
		logrus.Error(err)
		os.Exit(1)
	}

	// Override root RunE to show interactive home screen instead of help text
	rootCmd.RunE = runHomeScreen

	rootCmd.SetFlagErrorFunc(flagErrorFuncfunc)
	return rootCmd
}

func runHomeScreen(cmd *cobra.Command, args []string) error {
	// If args provided, fall through to standard error handling
	if len(args) > 0 {
		return validate.SubCommandExists(cmd, args)
	}

	// Self-heal stale registry entries
	projRegistry.SelfHeal()

	// Detect current working directory project
	cwd, _ := os.Getwd()

	// Build project summary and entries
	projects := projRegistry.All()
	summary := home.ProjectSummary{
		Types: make(map[string]int),
	}
	entries := make([]home.ProjectEntry, 0, len(projects))
	for name, p := range projects {
		summary.Total++
		summary.Types[p.Type]++
		status := "stopped"
		out, err := exec.Command("tainer", "pod", "inspect",
			"--format", "{{.State}}", fmt.Sprintf("tainer-%s", name)).CombinedOutput()
		if err == nil {
			status = strings.TrimSpace(string(out))
		}
		if status == "Running" {
			summary.Running++
		}
		entries = append(entries, home.ProjectEntry{
			Name:      name,
			Type:      p.Type,
			Domain:    p.Domain,
			Status:    status,
			Path:      p.Path,
			IsCurrent: p.Path == cwd,
		})
	}

	result, err := home.Run(summary, entries)
	if err != nil {
		return err
	}

	if result.Action == "" {
		return nil
	}

	cmdArgs := strings.Fields(result.Action)

	// For project-scoped commands, run from the project directory
	c := exec.Command("tainer", cmdArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if result.ProjectDir != "" {
		c.Dir = result.ProjectDir
	}
	return c.Run()
}

func flagErrorFuncfunc(c *cobra.Command, e error) error {
	e = fmt.Errorf("%w\nSee '%s --help'", e, c.CommandPath())
	return e
}

func addCommand(c registry.CliCommand) {
	parent := rootCmd
	if c.Parent != nil {
		parent = c.Parent
	}
	parent.AddCommand(c.Command)

	c.Command.SetFlagErrorFunc(flagErrorFuncfunc)

	// - templates need to be set here, as PersistentPreRunE() is
	// not called when --help is used.
	// - rootCmd uses cobra default template not ours
	c.Command.SetHelpTemplate(helpTemplate)
	c.Command.SetUsageTemplate(usageTemplate)
	c.Command.DisableFlagsInUseLine = true
}
