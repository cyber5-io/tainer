//go:build linux

package runtime

// canSpawnDaemon is always an error on Linux: cyberstackd is socket-activated
// by systemd there, so tainer never spawns it.
func canSpawnDaemon() error {
	return ErrEngineManaged
}
