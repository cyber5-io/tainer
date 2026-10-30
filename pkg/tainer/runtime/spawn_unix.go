//go:build !windows && !linux

package runtime

// canSpawnDaemon reports whether this OS can host the tainer engine.
func canSpawnDaemon() error {
	return nil
}
