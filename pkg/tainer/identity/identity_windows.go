//go:build windows

package identity

// DetectFromDir returns default uid/gid on Windows.
// Windows containers don't use Unix uid/gid — the values are passed
// to the VM which runs Linux internally.
func DetectFromDir(_ string) (uint32, uint32, error) {
	return 1000, 1000, nil
}
