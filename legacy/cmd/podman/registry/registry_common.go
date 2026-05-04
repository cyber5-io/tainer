//go:build !freebsd

package registry

// DefaultRootAPIPath is the default path of the REST socket
const DefaultRootAPIPath = "/run/tainer/tainer.sock"
