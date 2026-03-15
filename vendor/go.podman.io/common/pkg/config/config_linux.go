package config

import (
	selinux "github.com/opencontainers/selinux/go-selinux"
	"go.podman.io/common/pkg/capabilities"
)

const (
	// DefaultSignaturePolicyPath is the default value for the
	// policy.json file.
	DefaultSignaturePolicyPath = "/etc/containers/policy.json"
)

func selinuxEnabled() bool {
	return selinux.GetEnabled()
}

var defaultHelperBinariesDir = []string{
	"/opt/tainer/bin",
	"/usr/local/libexec/tainer",
	"/usr/local/lib/tainer",
	"/usr/libexec/tainer",
	"/usr/lib/tainer",
}

// Capabilities returns the capabilities parses the Add and Drop capability
// list from the default capabilities for the container.
func (c *Config) Capabilities(user string, addCapabilities, dropCapabilities []string) ([]string, error) {
	userNotRoot := func(user string) bool {
		if user == "" || user == "root" || user == "0" {
			return false
		}
		return true
	}

	defaultCapabilities := c.Containers.DefaultCapabilities.Get()
	if userNotRoot(user) {
		defaultCapabilities = []string{}
	}

	return capabilities.MergeCapabilities(defaultCapabilities, addCapabilities, dropCapabilities)
}
