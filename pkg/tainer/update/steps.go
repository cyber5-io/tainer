package update

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/project"
	tainerRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tls"
	"github.com/containers/podman/v6/pkg/tainer/tui"
	"github.com/containers/podman/v6/pkg/tainer/tui/progress"
	"github.com/containers/podman/v6/version/rawversion"
)

// RunCoreWithTUI runs the self-update with progress spinners.
func RunCoreWithTUI() error {
	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)

	currentVersion := rawversion.TainerVersion
	var release *githubRelease
	var remoteVersion string
	var tmpPath string

	steps := []progress.Step{
		{
			Label: "Checking for updates",
			Run: func() error {
				var err error
				release, err = getLatestRelease()
				if err != nil {
					return fmt.Errorf("checking for updates: %w", err)
				}
				remoteVersion = strings.TrimPrefix(release.TagName, "v")
				if remoteVersion == "" {
					return fmt.Errorf("could not determine remote version from tag %q", release.TagName)
				}
				return nil
			},
		},
		{
			Label: "Downloading update",
			Run: func() error {
				// Check if already up to date
				if remoteVersion == currentVersion {
					return nil
				}

				// Downgrade protection — abort silently, handled after TUI exits
				cmp := compareSemver(remoteVersion, currentVersion)
				if cmp < 0 {
					return nil
				}

				assetName := fmt.Sprintf("tainer-%s-%s", runtime.GOOS, runtime.GOARCH)
				downloadURL := findAsset(release, assetName)
				if downloadURL == "" {
					return fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
				}

				tmpFile, err := os.CreateTemp("", "tainer-update-*")
				if err != nil {
					return fmt.Errorf("creating temp file: %w", err)
				}
				tmpPath = tmpFile.Name()

				resp, err := ghRequest(downloadURL)
				if err != nil {
					tmpFile.Close()
					os.Remove(tmpPath)
					return fmt.Errorf("downloading: %w", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != 200 {
					tmpFile.Close()
					os.Remove(tmpPath)
					return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
				}

				if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxDownloadSize)); err != nil {
					tmpFile.Close()
					os.Remove(tmpPath)
					return fmt.Errorf("saving binary: %w", err)
				}
				tmpFile.Close()

				return os.Chmod(tmpPath, 0755)
			},
		},
	}

	footer := []string{
		mutedStyle.Render(fmt.Sprintf("  Current: v%s", currentVersion)),
	}
	result, err := progress.Run("Tainer Update", steps, footer)
	if err != nil {
		return err
	}
	if result.Err != nil {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
		return result.Err
	}

	// Already up to date
	if remoteVersion == currentVersion {
		tui.PrintWithLogo(tealStyle.Render("✓") + " " + textStyle.Render("Already up to date") + mutedStyle.Render(" (v"+currentVersion+")"))
		return nil
	}

	// Downgrade protection
	cmp := compareSemver(remoteVersion, currentVersion)
	if cmp < 0 {
		orangeStyle := lipgloss.NewStyle().Foreground(c.Orange).Bold(true)
		fmt.Printf("\n  %s %s\n",
			orangeStyle.Render("!"),
			textStyle.Render(fmt.Sprintf("Remote version (v%s) is older than current (v%s)", remoteVersion, currentVersion)))
		fmt.Printf("  %s ", textStyle.Render("Downgrade? [y/N]"))
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("  Cancelled.")
			return nil
		}
		fmt.Println()

		// Need to actually download now
		assetName := fmt.Sprintf("tainer-%s-%s", runtime.GOOS, runtime.GOARCH)
		downloadURL := findAsset(release, assetName)
		if downloadURL == "" {
			return fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
		}
		tmpFile, err := os.CreateTemp("", "tainer-update-*")
		if err != nil {
			return fmt.Errorf("creating temp file: %w", err)
		}
		tmpPath = tmpFile.Name()
		resp, err := ghRequest(downloadURL)
		if err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("downloading: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
		}
		if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxDownloadSize)); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("saving binary: %w", err)
		}
		tmpFile.Close()
		os.Chmod(tmpPath, 0755)
	}

	// Install step — needs sudo so runs outside TUI
	defer os.Remove(tmpPath)
	fmt.Printf("\n  %s\n", mutedStyle.Render(fmt.Sprintf("Installing to %s (requires sudo)...", tainerBinaryPath)))

	stagingPath := tainerBinaryPath + ".new"
	cpCmd := exec.Command("sudo", "cp", tmpPath, stagingPath)
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	cpCmd.Stdin = os.Stdin
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("staging binary: %w", err)
	}

	mvCmd := exec.Command("sudo", "mv", stagingPath, tainerBinaryPath)
	mvCmd.Stdout = os.Stdout
	mvCmd.Stderr = os.Stderr
	if err := mvCmd.Run(); err != nil {
		return fmt.Errorf("installing binary: %w", err)
	}

	tui.PrintWithLogo(tealStyle.Render("✓") + " " + textStyle.Render("Updated: ") +
		mutedStyle.Render(fmt.Sprintf("v%s → v%s", currentVersion, remoteVersion)))
	return nil
}

// RunImagesWithTUI pulls latest images for a project with progress spinners.
func RunImagesWithTUI(projectName string) error {
	c := tui.Colors()
	tealStyle := lipgloss.NewStyle().Foreground(c.Teal)
	textStyle := lipgloss.NewStyle().Foreground(c.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)

	var projectDir string
	var m *manifest.Manifest

	if projectName == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}
		if !manifest.Exists(cwd) {
			c := tui.Colors()
			labelStyle := lipgloss.NewStyle().Bold(true).Foreground(c.Blue)
			mutedStyle := lipgloss.NewStyle().Foreground(c.Muted)
			content := labelStyle.Render("Usage:") + "\n" +
				mutedStyle.Render("  tainer update          ") + lipgloss.NewStyle().Foreground(c.Text).Render("Pull latest images for the current project") + "\n" +
				mutedStyle.Render("  tainer update <name>   ") + lipgloss.NewStyle().Foreground(c.Text).Render("Pull latest images for a named project") + "\n" +
				mutedStyle.Render("  tainer update core     ") + lipgloss.NewStyle().Foreground(c.Text).Render("Self-update the tainer binary")
			tui.PrintWithLogo(content)
			return nil
		}
		projectDir = cwd
		name, found := tainerRegistry.FindByPath(cwd)
		if found {
			projectName = name
		}
	} else {
		p, ok := tainerRegistry.Get(projectName)
		if !ok {
			return fmt.Errorf("project %q not found in registry — start it first with 'tainer start'", projectName)
		}
		projectDir = p.Path
	}

	var err error
	m, err = manifest.LoadFromDir(projectDir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}
	if projectName == "" {
		projectName = m.Project.Name
	}

	steps := []progress.Step{
		{
			Label: "Pulling latest images",
			Run: func() error {
				return project.PullImages(m)
			},
		},
		{
			Label: "Updating templates",
			Run: func() error {
				release, err := getLatestRelease()
				if err != nil {
					return nil // non-fatal, templates are optional
				}
				templatesURL := findAsset(release, "templates.zip")
				if templatesURL != "" {
					downloadAndExtractZip(templatesURL, config.TemplatesDir())
				}
				return nil
			},
		},
		{
			Label: "Checking TLS certificate",
			Run: func() error {
				release, err := getLatestRelease()
				if err != nil {
					return nil
				}
				certURL := findAsset(release, "tainer.me.crt")
				keyURL := findAsset(release, "tainer.me.key")
				if certURL != "" && keyURL != "" {
					tls.DownloadCert(certURL, config.CertFile(), keyURL, config.KeyFile())
				}
				return nil
			},
		},
	}

	podName := fmt.Sprintf("tainer-%s", projectName)
	running := project.IsPodRunning(podName)

	var footer []string
	if running {
		footer = []string{
			tealStyle.Render("✓") + " " + textStyle.Render(projectName) + mutedStyle.Render(" images updated"),
			"  " + mutedStyle.Render("Restart to apply: tainer restart"),
		}
	} else {
		footer = []string{
			tealStyle.Render("✓") + " " + textStyle.Render(projectName) + mutedStyle.Render(" images updated"),
		}
	}

	result, err := progress.Run("Updating "+projectName, steps, footer)
	if err != nil {
		return err
	}
	return result.Err
}
