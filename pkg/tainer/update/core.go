package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/containers/podman/v6/version/rawversion"
)

const tainerBinaryPath = "/opt/tainer/bin/tainer"

// RunCore self-updates the tainer binary from GitHub Releases.
func RunCore() error {
	currentVersion := rawversion.TainerVersion
	fmt.Printf("Current version: v%s\n", currentVersion)
	fmt.Println("Checking for updates...")

	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}

	// Parse remote version from tag (strip leading "v" if present)
	remoteVersion := strings.TrimPrefix(release.TagName, "v")
	if remoteVersion == "" {
		return fmt.Errorf("could not determine remote version from tag %q", release.TagName)
	}

	if remoteVersion == currentVersion {
		fmt.Printf("Already up to date (v%s)\n", currentVersion)
		return nil
	}

	fmt.Printf("New version available: v%s -> v%s\n", currentVersion, remoteVersion)

	// Find the matching binary asset
	assetName := fmt.Sprintf("tainer-%s-%s", runtime.GOOS, runtime.GOARCH)
	downloadURL := findAsset(release, assetName)
	if downloadURL == "" {
		return fmt.Errorf("no release asset found for %s (expected %q)", runtime.GOOS+"/"+runtime.GOARCH, assetName)
	}

	// Download to temp file
	fmt.Println("Downloading...")
	tmpFile, err := os.CreateTemp("", "tainer-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	resp, err := ghRequest(downloadURL)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("downloading binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		tmpFile.Close()
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxDownloadSize)); err != nil {
		tmpFile.Close()
		return fmt.Errorf("saving binary: %w", err)
	}
	tmpFile.Close()

	// Make temp file executable
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	// Install using atomic rename to avoid killing the running binary.
	// cp overwrites the inode in-place, which crashes the current process on macOS.
	// Instead: cp to a staging path, then mv (atomic rename) over the target.
	stagingPath := tainerBinaryPath + ".new"
	fmt.Printf("Installing to %s (requires sudo)...\n", tainerBinaryPath)

	cpCmd := exec.Command("sudo", "cp", tmpFile.Name(), stagingPath)
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

	fmt.Printf("Updated: v%s -> v%s\n", currentVersion, remoteVersion)
	return nil
}
