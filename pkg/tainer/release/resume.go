package release

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/filelock"
)

// ResumeEntry is one pod that was running when an update began.
type ResumeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ResumeFile records which pods to bring back after an update.
type ResumeFile struct {
	ForVersion  string        `json:"forVersion"`
	FromVersion string        `json:"fromVersion"`
	WrittenAt   time.Time     `json:"writtenAt"`
	Pods        []ResumeEntry `json:"pods"`
}

// WriteResume stores f atomically (0600).
func WriteResume(f ResumeFile) error {
	path := config.ResumeFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-resume-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ReadResume returns the stored file; ok is false when none exists.
func ReadResume() (ResumeFile, bool, error) {
	data, err := os.ReadFile(config.ResumeFile())
	if errors.Is(err, os.ErrNotExist) {
		return ResumeFile{}, false, nil
	}
	if err != nil {
		return ResumeFile{}, false, err
	}
	var f ResumeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return ResumeFile{}, false, err
	}
	return f, true, nil
}

// ClearResume removes the file; a missing file is not an error.
func ClearResume() error {
	if err := os.Remove(config.ResumeFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// WithResumeLock runs fn while holding an exclusive lock next to the resume
// file, so concurrent resumers don't both restart the pods.
func WithResumeLock(fn func() error) error {
	release, err := filelock.Lock(config.ResumeFile() + ".lock")
	if err != nil {
		return err
	}
	defer release()
	return fn()
}
