package machine

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// IsInitialized checks if a tainer machine exists.
func IsInitialized() bool {
	cmd := exec.Command("tainer", "machine", "ls", "--format", "{{.Name}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

// IsRunning checks if the default tainer machine is running.
func IsRunning() bool {
	cmd := exec.Command("tainer", "machine", "ls", "--format", "{{.Running}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// EnsureRunning makes sure the machine is initialized and started.
// On Linux, podman runs natively — no machine needed.
func EnsureRunning() error {
	if runtime.GOOS == "linux" {
		return nil
	}

	if IsRunning() {
		return nil
	}

	if !IsInitialized() {
		fmt.Println("Running Tainer for the first time, initializing...")
		initCmd := exec.Command("tainer", "machine", "init")
		initCmd.Stdout = os.Stdout
		initCmd.Stderr = os.Stderr
		if err := initCmd.Run(); err != nil {
			return fmt.Errorf("machine init failed: %w", err)
		}

		// Set rootful mode for port binding
		setCmd := exec.Command("tainer", "machine", "set", "--rootful")
		setCmd.Stdout = os.Stdout
		setCmd.Stderr = os.Stderr
		if err := setCmd.Run(); err != nil {
			return fmt.Errorf("machine set --rootful failed: %w", err)
		}
	}

	fmt.Println("Starting Tainer machine...")
	startCmd := exec.Command("tainer", "machine", "start")
	startCmd.Stdout = os.Stdout
	startCmd.Stderr = os.Stderr
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("machine start failed: %w", err)
	}

	return nil
}
