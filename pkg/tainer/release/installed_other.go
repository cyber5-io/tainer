//go:build !darwin

package release

// InstalledTainer is macOS-only (pkg receipts); elsewhere it reports none.
func InstalledTainer(run Runner) string { return "" }

// InstalledMenu is macOS-only; elsewhere it reports none.
func InstalledMenu() string { return "" }
