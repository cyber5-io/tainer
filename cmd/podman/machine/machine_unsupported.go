//go:build !amd64 && !arm64

package machine

// init do not register _tainer machine_ command on unsupported platforms
func init() {}
