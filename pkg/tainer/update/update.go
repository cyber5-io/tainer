package update

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/project"
	tainerRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/tls"
	"github.com/containers/podman/v6/pkg/tainer/tui"
)

const (
	ghReleasesAPI   = "https://api.github.com/repos/cyber5-io/tainer/releases/latest"
	maxDownloadSize = 100 * 1024 * 1024 // 100 MB
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// RunTemplates downloads the latest templates and TLS cert from GitHub Releases.
// This is the original update behavior (templates + TLS).
func RunTemplates() error {
	fmt.Println("Checking for updates...")

	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("fetching latest release: %w", err)
	}

	// Update templates
	templatesURL := findAsset(release, "templates.zip")
	if templatesURL != "" {
		fmt.Println("Downloading templates...")
		if err := downloadAndExtractZip(templatesURL, config.TemplatesDir()); err != nil {
			return fmt.Errorf("updating templates: %w", err)
		}
		fmt.Println("Templates updated")
	}

	// Update TLS cert
	certURL := findAsset(release, "tainer.me.crt")
	keyURL := findAsset(release, "tainer.me.key")
	if certURL != "" && keyURL != "" {
		fmt.Println("Downloading TLS certificate...")
		if err := tls.DownloadCert(certURL, config.CertFile(), keyURL, config.KeyFile()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update TLS cert: %v\n", err)
		} else {
			fmt.Println("TLS certificate updated")
		}
	}

	return nil
}

// RunImages pulls latest images for a project.
// If projectName is empty, it attempts to detect the project from the current directory.
func RunImages(projectName string) error {
	var projectDir string

	if projectName == "" {
		// Detect from current directory
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		if !manifest.Exists(cwd) {
			return tui.StyledError("Not in a Tainer project directory.\nUsage: tainer update [project-name|core]")
		}
		projectDir = cwd
		name, found := tainerRegistry.FindByPath(cwd)
		if found {
			projectName = name
		}
	} else {
		// Look up named project in registry
		p, ok := tainerRegistry.Get(projectName)
		if !ok {
			return fmt.Errorf("project %q not found in registry — start it first with 'tainer start'", projectName)
		}
		projectDir = p.Path
	}

	// Load manifest
	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	if projectName == "" {
		projectName = m.Project.Name
	}

	fmt.Printf("Pulling latest images for %s...\n", projectName)
	if err := project.PullImagesVerbose(m); err != nil {
		return fmt.Errorf("pulling images: %w", err)
	}

	// Check if pod is running
	podName := fmt.Sprintf("tainer-%s", projectName)
	if project.IsPodRunning(podName) {
		fmt.Print("Images updated. Restart to apply? (y/n) ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" || answer == "yes" {
			fmt.Println("Restart the project with: tainer restart")
		}
	}

	return nil
}

func ghRequest(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tainer/1.0")
	return httpClient.Do(req)
}

func getLatestRelease() (*githubRelease, error) {
	resp, err := ghRequest(ghReleasesAPI)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func findAsset(release *githubRelease, name string) string {
	for _, a := range release.Assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

func downloadAndExtractZip(url, destDir string) error {
	tmpFile, err := os.CreateTemp("", "tainer-templates-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	resp, err := ghRequest(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxDownloadSize)); err != nil {
		return err
	}
	tmpFile.Close()

	r, err := zip.OpenReader(tmpFile.Name())
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		path := filepath.Join(destDir, f.Name)
		// Prevent zip slip
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)) {
			continue
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(path), 0755)
		outFile, err := os.Create(path)
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
	}
	return nil
}
