package system

import (
	"fmt"
	"os"
	"strings"

	"github.com/containers/podman/v6/cmd/podman/common"
	"github.com/containers/podman/v6/cmd/podman/registry"
	"github.com/containers/podman/v6/cmd/podman/validate"
	"github.com/containers/podman/v6/pkg/domain/entities"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/containers/podman/v6/version/rawversion"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/common/pkg/completion"
	"go.podman.io/common/pkg/report"
)

var (
	versionCommand = &cobra.Command{
		Use:               "version [options]",
		Args:              validate.NoArgs,
		Short:             "Display the Tainer version information",
		RunE:              version,
		ValidArgsFunction: completion.AutocompleteNone,
		Annotations: map[string]string{
			registry.ParentNSRequired: "",
		},
	}
	versionFormat string
)

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: versionCommand,
	})
	flags := versionCommand.Flags()

	formatFlagName := "format"
	flags.StringVarP(&versionFormat, formatFlagName, "f", "", "Change the output format to JSON or a Go template")
	_ = versionCommand.RegisterFlagCompletionFunc(formatFlagName, common.AutocompleteFormat(&entities.SystemVersionReport{}))
}

func version(cmd *cobra.Command, _ []string) error {
	versions, err := registry.ContainerEngine().Version(registry.Context())
	if err != nil {
		return err
	}

	if report.IsJSON(versionFormat) {
		s, err := json.MarshalToString(versions)
		if err != nil {
			return err
		}
		fmt.Println(s)
		return nil
	}

	if cmd.Flag("format").Changed {
		rpt := report.New(os.Stdout, cmd.Name())
		defer rpt.Flush()

		// Use OriginUnknown so it does not add an extra range since it
		// will only be called for a single element and not a slice.
		rpt, err = rpt.Parse(report.OriginUnknown, versionFormat)
		if err != nil {
			return err
		}
		if err := rpt.Execute(versions); err != nil {
			// only log at debug since we fall back to the client only template
			logrus.Debugf("Failed to execute template: %v", err)
			// On Failure, assume user is using older version of podman version --format and check client
			versionFormat = strings.ReplaceAll(versionFormat, ".Server.", ".")
			rpt, err := rpt.Parse(report.OriginUnknown, versionFormat)
			if err != nil {
				return err
			}
			if err := rpt.Execute(versions.Client); err != nil {
				return err
			}
		}
		return nil
	}

	labelStyle := tui.LabelStyle()
	valueStyle := tui.TextStyle()
	mutedStyle := tui.SubtitleStyle()

	cv := versions.Client
	var infoLines []string
	infoLines = append(infoLines, fmt.Sprintf("%s  %s",
		tui.TitleStyle().Render("tainer"),
		valueStyle.Render("v"+rawversion.TainerVersion)))
	infoLines = append(infoLines, fmt.Sprintf("%s  %s", labelStyle.Render("Engine  "), mutedStyle.Render("Podman "+cv.Version)))
	infoLines = append(infoLines, fmt.Sprintf("%s  %s", labelStyle.Render("Go      "), mutedStyle.Render(cv.GoVersion)))
	infoLines = append(infoLines, fmt.Sprintf("%s  %s", labelStyle.Render("Built   "), mutedStyle.Render(cv.BuiltTime)))
	infoLines = append(infoLines, fmt.Sprintf("%s  %s", labelStyle.Render("OS/Arch "), mutedStyle.Render(cv.OsArch)))

	tui.PrintWithLogo(strings.Join(infoLines, "\n"))
	return nil
}
