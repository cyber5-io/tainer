package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BackupsDir returns the path to the backups directory.
func BackupsDir() string {
	return filepath.Join(BaseDir(), "backups")
}

// BackupProjectDir returns the path for a specific project's backup directory.
func BackupProjectDir(projectName string) string {
	return filepath.Join(BackupsDir(), projectName)
}

// BackupMetadata holds information about a config backup.
type BackupMetadata struct {
	Project  string `json:"project"`
	Path     string `json:"path"`
	BackedUp string `json:"backed_up"`
}

// configFiles lists all project config files to back up.
var configFiles = []string{
	"tainer.yaml",
	".env",
	".tainer-authorized_keys",
	".tainer.local.yaml",
}

// Backup copies project config files from projectDir into the backup location
// for the given project name. It overwrites any previous backup.
func Backup(projectName, projectDir string) error {
	backupDir := BackupProjectDir(projectName)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}

	// tainer.yaml is required
	yamlSrc := filepath.Join(projectDir, "tainer.yaml")
	if err := copyFile(yamlSrc, filepath.Join(backupDir, "tainer.yaml")); err != nil {
		return fmt.Errorf("backing up tainer.yaml: %w", err)
	}

	// Back up optional config files
	for _, name := range configFiles[1:] {
		src := filepath.Join(projectDir, name)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(backupDir, name)); err != nil {
				return fmt.Errorf("backing up %s: %w", name, err)
			}
		}
	}

	// Write metadata
	meta := BackupMetadata{
		Project:  projectName,
		Path:     projectDir,
		BackedUp: time.Now().UTC().Format(time.RFC3339),
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "metadata.json"), metaData, 0644); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	return nil
}

// Restore copies all backed-up config files back into projectDir.
// Returns the list of files that were restored.
func Restore(projectName, projectDir string) ([]string, error) {
	backupDir := BackupProjectDir(projectName)

	yamlSrc := filepath.Join(backupDir, "tainer.yaml")
	if _, err := os.Stat(yamlSrc); err != nil {
		return nil, fmt.Errorf("no backup found for project %q", projectName)
	}

	var restored []string
	for _, name := range configFiles {
		src := filepath.Join(backupDir, name)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(projectDir, name)); err != nil {
				return restored, fmt.Errorf("restoring %s: %w", name, err)
			}
			restored = append(restored, name)
		}
	}

	return restored, nil
}

// RestoreFiles copies only the specified files from backup into projectDir.
func RestoreFiles(projectName, projectDir string, files []string) ([]string, error) {
	backupDir := BackupProjectDir(projectName)
	var restored []string
	for _, name := range files {
		src := filepath.Join(backupDir, name)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(projectDir, name)); err != nil {
				return restored, fmt.Errorf("restoring %s: %w", name, err)
			}
			restored = append(restored, name)
		}
	}
	return restored, nil
}

// MissingWithBackup returns config files that are missing from projectDir but exist in backup.
func MissingWithBackup(projectName, projectDir string) []string {
	backupDir := BackupProjectDir(projectName)
	var missing []string
	for _, name := range configFiles {
		projPath := filepath.Join(projectDir, name)
		backupPath := filepath.Join(backupDir, name)
		if _, err := os.Stat(projPath); os.IsNotExist(err) {
			if _, err := os.Stat(backupPath); err == nil {
				missing = append(missing, name)
			}
		}
	}
	return missing
}

// BackedUpFiles returns which config files exist in the backup for a project.
func BackedUpFiles(projectName string) []string {
	backupDir := BackupProjectDir(projectName)
	var files []string
	for _, name := range configFiles {
		if _, err := os.Stat(filepath.Join(backupDir, name)); err == nil {
			files = append(files, name)
		}
	}
	return files
}

// BackupExists checks whether a backup exists for the given project name.
func BackupExists(projectName string) bool {
	yamlPath := filepath.Join(BackupProjectDir(projectName), "tainer.yaml")
	_, err := os.Stat(yamlPath)
	return err == nil
}

// LoadBackupMetadata reads the metadata for a project backup.
func LoadBackupMetadata(projectName string) (*BackupMetadata, error) {
	metaPath := filepath.Join(BackupProjectDir(projectName), "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var meta BackupMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// FindBackupForPath returns the project name if any backup's metadata.path matches
// the given directory. Returns ("", false) if no match.
func FindBackupForPath(dir string) (string, bool) {
	backupsDir := BackupsDir()
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := LoadBackupMetadata(entry.Name())
		if err != nil {
			continue
		}
		if meta.Path == dir {
			return meta.Project, true
		}
	}
	return "", false
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
