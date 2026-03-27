package update

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
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

	cmp := compareSemver(remoteVersion, currentVersion)
	if cmp == 0 {
		fmt.Printf("Already up to date (v%s)\n", currentVersion)
		return nil
	}

	if cmp < 0 {
		// Remote is older than current — possible downgrade
		fmt.Printf("Warning: remote version (v%s) is older than current (v%s)\n", remoteVersion, currentVersion)
		fmt.Print("Downgrade to this version? (y/n) ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Update cancelled.")
			return nil
		}
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

	// Install: cp to staging path, then mv over target.
	// macOS kills the running process when the binary is replaced, so we
	// print the success message BEFORE the mv and exit immediately after.
	stagingPath := tainerBinaryPath + ".new"
	fmt.Printf("Installing to %s (requires sudo)...\n", tainerBinaryPath)

	cpCmd := exec.Command("sudo", "cp", tmpFile.Name(), stagingPath)
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	cpCmd.Stdin = os.Stdin
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("staging binary: %w", err)
	}

	// Print success BEFORE mv — the process will be killed when the binary is replaced.
	fmt.Printf("\nUpdated: v%s → v%s\n", currentVersion, remoteVersion)

	// This mv will kill the current process on macOS. That's expected.
	exec.Command("sudo", "mv", stagingPath, tainerBinaryPath).Run()
	os.Exit(0)
	return nil
}

// compareSemver compares two semver strings (without "v" prefix).
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareSemver(a, b string) int {
	aParts := parseSemverParts(a)
	bParts := parseSemverParts(b)
	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

func parseSemverParts(v string) [3]int {
	// Strip any pre-release suffix (e.g. "1.0.0-dev" → "1.0.0")
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		result[i], _ = strconv.Atoi(parts[i])
	}
	return result
}
