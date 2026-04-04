package gitsetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// dirIgnoreContent is placed inside data/ and db/ so the directories
// are tracked by git but their contents are not.
const dirIgnoreContent = "*\n!.gitignore\n"

// tainerIgnoreEntries are the root .gitignore lines that tainer manages.
// Each entry is machine-specific and must never be committed.
var tainerIgnoreEntries = []string{
	".env",
	".tainer-authorized_keys",
	".tainer.local.yaml",
}

const tainerHeader = "# Tainer (local dev)"

// IsGitRepo returns true if the directory is inside a git repository.
func IsGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// WriteDirIgnore drops a .gitignore inside the given directory so the
// directory itself is tracked but its contents are not.
func WriteDirIgnore(dir string) error {
	return os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(dirIgnoreContent), 0644)
}

// EnsureRootIgnore ensures the root .gitignore contains all tainer entries.
// If the file does not exist it is created. Existing content is preserved.
func EnsureRootIgnore(projectDir string) error {
	path := filepath.Join(projectDir, ".gitignore")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	content := string(existing)
	var missing []string
	for _, entry := range tainerIgnoreEntries {
		if !containsLine(content, entry) {
			missing = append(missing, entry)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	var block strings.Builder
	// Add separator if file has existing content
	if len(content) > 0 && !strings.HasSuffix(content, "\n\n") {
		if !strings.HasSuffix(content, "\n") {
			block.WriteString("\n")
		}
		block.WriteString("\n")
	}
	block.WriteString(tainerHeader + "\n")
	for _, entry := range missing {
		block.WriteString(entry + "\n")
	}

	return os.WriteFile(path, []byte(content+block.String()), 0644)
}

// InitRepo runs git init in the given directory, then creates .gitignore
// with tainer entries.
func InitRepo(projectDir string) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = projectDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %s", strings.TrimSpace(string(output)))
	}
	return EnsureRootIgnore(projectDir)
}

// containsLine checks if a line appears in the content as a standalone line.
func containsLine(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}
