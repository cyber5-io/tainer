package pod

import (
	"errors"
	"fmt"
	"strings"
)

// BusyRemovalError is a pod member that couldn't be removed because its
// files were still in use (tain-101: the runtime reports "replacing mount
// point ... device or resource busy"). Error() is the plain explanation shown
// to users; the raw runtime error stays reachable through Unwrap and Detail.
type BusyRemovalError struct {
	Pod  string
	Role string
	Verb string // "replace" (update/start) or "remove" (destroy)
	Err  error
}

func (e *BusyRemovalError) Error() string {
	return fmt.Sprintf(`couldn't %s %s's %s — its files are still in use. Run "tainer restart %s" or try again in a minute; if it keeps happening, restart tainer from the menu bar.`,
		e.Verb, e.Pod, e.Role, e.Pod)
}

func (e *BusyRemovalError) Unwrap() error { return e.Err }

// Detail is the raw runtime error, for logs and diagnostics.
func (e *BusyRemovalError) Detail() string { return e.Err.Error() }

// isBusyRemoval reports whether a removal failed because the member's files
// were still in use.
func isBusyRemoval(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "replacing mount point") || strings.Contains(msg, "device or resource busy")
}

// removeErr turns a failed removal of c into the error the user sees: a
// BusyRemovalError when its files were in use, otherwise "remove <name>: err".
func removeErr(podName string, c Container, verb string, err error) error {
	if err == nil {
		return nil
	}
	if !isBusyRemoval(err) {
		return fmt.Errorf("remove %s: %w", c.Name, err)
	}
	role := c.Role
	if role == "" {
		role = strings.TrimPrefix(c.Name, ContainerName(podName, ""))
	}
	return &BusyRemovalError{Pod: podName, Role: role, Verb: verb, Err: err}
}

// asBusyRemoval returns err's BusyRemovalError, if any.
func asBusyRemoval(err error) (*BusyRemovalError, bool) {
	var b *BusyRemovalError
	ok := errors.As(err, &b)
	return b, ok
}
