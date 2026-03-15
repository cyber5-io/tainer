package config

const (
	// DefaultSignaturePolicyPath is the default value for the
	// policy.json file.
	DefaultSignaturePolicyPath = "/etc/containers/policy.json"
)

var defaultHelperBinariesDir = []string{
	// Tainer install path (primary)
	"/opt/tainer/bin",
	// Relative to the binary directory
	"$BINDIR/../libexec/tainer",
	// Homebrew install paths
	"/usr/local/opt/tainer/libexec/tainer",
	"/opt/homebrew/opt/tainer/libexec/tainer",
	"/opt/homebrew/bin",
	"/usr/local/bin",
	// default paths
	"/usr/local/libexec/tainer",
	"/usr/local/lib/tainer",
	"/usr/libexec/tainer",
	"/usr/lib/tainer",
}
