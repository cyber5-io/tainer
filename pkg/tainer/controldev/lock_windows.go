//go:build windows

package controldev

import (
	"os"
	"time"
)

// lockFile on Windows uses an exclusive create, retried; the runtime is not
// supported there yet, so this only has to be correct, not crash-robust.
func lockFile(path string) (func(), error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return func() { f.Close(); os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
