package project

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/containers/podman/v6/pkg/tainer/manifest"
)

// OpenBrowser opens the project URL in the default browser.
func OpenBrowser(projectDir string) error {
	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://%s", m.Project.Domain)
	return openURL(url)
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
