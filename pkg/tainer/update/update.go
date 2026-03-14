package update

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/tls"
)

const (
	ghReleasesAPI = "https://api.github.com/repos/cyber5-io/tainer/releases/latest"
)

type githubRelease struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// Run downloads the latest templates and TLS cert from GitHub Releases.
func Run() error {
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

func getLatestRelease() (*githubRelease, error) {
	resp, err := http.Get(ghReleasesAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
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
