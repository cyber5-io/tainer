//go:build !darwin

package release

// InstalledMenu is macOS-only; elsewhere it reports none.
func InstalledMenu() string { return "" }
