//go:build darwin

package doctor

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const loginItemApplies = true

// menuBinary is the installed menu app. Its --login-item-status mode prints
// the status and exits without starting the menu (SMAppService answers only
// for the bundle it runs from, so the app is asked itself).
const (
	menuApp    = "/Applications/Tainer Menu.app"
	menuBinary = menuApp + "/Contents/MacOS/TainerMenu"
)

func init() {
	loginItemProbe = func(ctx context.Context) (string, bool, error) {
		if _, err := os.Stat(menuBinary); err != nil {
			return "", false, nil
		}
		// A menu older than the flag would ignore it and start a second
		// menu instead, so its version is read first.
		out, err := exec.CommandContext(ctx, "plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-", menuApp+"/Contents/Info.plist").Output()
		if err != nil {
			return "", true, err
		}
		if v := strings.TrimSpace(string(out)); !menuReportsLoginItem(v) {
			return outdatedPrefix + v, true, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, err = exec.CommandContext(ctx, menuBinary, "--login-item-status").Output()
		if err != nil {
			return "", true, err
		}
		return strings.TrimSpace(string(out)), true, nil
	}
}

// macOSMajor is the running macOS major version (14, 15, …); 0 if unknown.
func macOSMajor() int {
	v, err := unix.Sysctl("kern.osproductversion")
	if err != nil {
		return 0
	}
	major, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return major
}
