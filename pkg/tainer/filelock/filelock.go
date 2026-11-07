// Package filelock serialises tainer processes around shared state files
// (the project registry, the pod slots, the control pod). On Unix it is an
// flock that dies with the process; on Windows an exclusive-create file.
package filelock

import (
	"os"
	"path/filepath"
)

// Lock blocks until it holds the exclusive lock on path, creating the file
// (and its folder) when needed.
func Lock(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return lock(path)
}

// TryLock takes the exclusive lock on path without waiting. ok is false when
// another process holds it.
func TryLock(path string) (release func(), ok bool, err error) {
	return tryLock(path)
}
