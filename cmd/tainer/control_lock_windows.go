//go:build windows

package main

import (
	"os"
	"syscall"
)

// tryLock on Windows uses an exclusive create; the runtime is not supported
// there yet, so this only has to be correct, not robust to crashes.
func tryLock(path string) (release func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close(); os.Remove(path) }, true, nil
}

func detachedProcAttr() *syscall.SysProcAttr { return nil }
