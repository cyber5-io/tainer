//go:build !darwin

package doctor

// The menu app and its login item exist on macOS only.
const loginItemApplies = false

func macOSMajor() int { return 0 }
