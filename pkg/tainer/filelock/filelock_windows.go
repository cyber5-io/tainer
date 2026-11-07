//go:build windows

package filelock

import (
	"os"
	"time"
)

// The runtime is not supported on Windows yet, so this only has to be
// correct, not robust to crashes: a crashed holder leaves the file behind.

func lock(path string) (func(), error) {
	for {
		release, ok, err := tryLock(path)
		if err != nil || ok {
			return release, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func tryLock(path string) (func(), bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close(); os.Remove(path) }, true, nil
}
