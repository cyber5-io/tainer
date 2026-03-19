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

// Backup copies tainer.yaml and .env from projectDir into the backup location
// for the given project name. It overwrites any previous backup.
func Backup(projectName, projectDir string) error {
	backupDir := BackupProjectDir(projectName)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}

	// Copy tainer.yaml
	yamlSrc := filepath.Join(projectDir, "tainer.yaml")
	if err := copyFile(yamlSrc, filepath.Join(backupDir, "tainer.yaml")); err != nil {
		return fmt.Errorf("backing up tainer.yaml: %w", err)
	}

	// Copy .env (optional — not all projects have one)
	envSrc := filepath.Join(projectDir, ".env")
	if _, err := os.Stat(envSrc); err == nil {
		if err := copyFile(envSrc, filepath.Join(backupDir, ".env")); err != nil {
			return fmt.Errorf("backing up .env: %w", err)
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

// Restore copies backed-up tainer.yaml and .env back into projectDir.
func Restore(projectName, projectDir string) error {
	backupDir := BackupProjectDir(projectName)

	yamlSrc := filepath.Join(backupDir, "tainer.yaml")
	if _, err := os.Stat(yamlSrc); err != nil {
		return fmt.Errorf("no backup found for project %q", projectName)
	}

	if err := copyFile(yamlSrc, filepath.Join(projectDir, "tainer.yaml")); err != nil {
		return fmt.Errorf("restoring tainer.yaml: %w", err)
	}

	envSrc := filepath.Join(backupDir, ".env")
	if _, err := os.Stat(envSrc); err == nil {
		if err := copyFile(envSrc, filepath.Join(projectDir, ".env")); err != nil {
			return fmt.Errorf("restoring .env: %w", err)
		}
	}

	return nil
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
