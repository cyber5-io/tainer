package manifest

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// PersistV2Migration rewrites a legacy (v1 / unversioned) manifest at path to
// its canonical v2 form on disk, so the in-memory migration in ParseBytes runs
// once rather than on every load. The original file is copied to
// "<path>.v1.bak" before it is overwritten; marshaling drops comments and
// reformats, so the backup is the user's record of the pre-migration file.
//
// It is a no-op (migrated=false, err=nil) when the file is already v2. It is
// best-effort: callers should log a returned error and continue, never abort a
// start/restart over it. Only mutating lifecycle commands (start, restart)
// should call this — read-only loads must not rewrite the user's file.
func PersistV2Migration(path string) (migrated bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	// Peek at the on-disk version without running a full migration. An absent
	// version field (0) is legacy and defaults to 1, same as ParseBytes.
	var probe struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return false, fmt.Errorf("reading manifest version: %w", err)
	}
	if probe.Version == 2 {
		return false, nil // already canonical — nothing to persist
	}

	// Run the real migration + validation. If the legacy file is invalid we
	// surface the error and leave the file untouched (no backup, no rewrite).
	m, err := ParseBytes(raw)
	if err != nil {
		return false, err
	}
	if m.Version != 2 {
		return false, nil // unexpected, but never rewrite to a non-v2 shape
	}

	// Back up the original verbatim, then write the canonical v2 form.
	if err := os.WriteFile(path+".v1.bak", raw, 0644); err != nil {
		return false, fmt.Errorf("writing backup: %w", err)
	}
	if err := Save(m, path); err != nil {
		return false, fmt.Errorf("writing migrated manifest: %w", err)
	}
	return true, nil
}
