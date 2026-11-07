package main

import "github.com/cyber5-io/tainer/pkg/tainer/filelock"

// tryLock takes an exclusive, non-blocking lock on path. ok is false when
// another process holds it. The lock dies with the process (Unix).
func tryLock(path string) (release func(), ok bool, err error) {
	return filelock.TryLock(path)
}
