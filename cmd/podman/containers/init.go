package containers

import (
	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/cmd/podman/validate"
	tainerCli "github.com/containers/podman/v6/pkg/tainer/cli"
	"github.com/spf13/cobra"
)

var (
	initCommand = &cobra.Command{
		Use:   "init [options]",
		Short: "Create a new Tainer project",
		Long:  "Create a new Tainer project in the current directory. Without flags, launches an interactive wizard.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Tainer fully owns `tainer init` — skip Podman engine setup
			return nil
		},
		RunE:              tainerCli.RunInit,
		Args:              validate.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
	}
)

var tainerInitOpts tainerCli.InitOptions

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: initCommand,
	})

	flags := initCommand.Flags()
	flags.StringVar(&tainerInitOpts.Name, "name", "", "Project name (required for non-interactive)")
	flags.StringVar(&tainerInitOpts.Type, "type", "", "Project type: wordpress, php, nodejs, nextjs, nuxtjs, nestjs, react, kompozi")
	flags.StringVar(&tainerInitOpts.PHP, "php", "", "PHP version (default: 8.4)")
	flags.StringVar(&tainerInitOpts.Node, "node", "", "Node.js version (default: 22)")
	flags.StringVar(&tainerInitOpts.DB, "db", "", "Database: mariadb, postgres, none (default depends on type)")
	flags.StringVar(&tainerInitOpts.Subdomain, "subdomain", "", "Subdomain for .tainer.me (default: project name)")
	flags.BoolVarP(&tainerInitOpts.Quiet, "quiet", "q", false, "Suppress output")
	flags.BoolVar(&tainerInitOpts.Start, "start", false, "Start the project after creation")
	flags.BoolVar(&tainerInitOpts.GitInit, "git-init", false, "Initialise a git repository")

	// Hide Podman's inherited flags that don't apply
	initCommand.SetHelpFunc(tainerCli.InitHelp)
}
