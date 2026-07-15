package ssh

import (
	"fmt"
	"os"
)

const (
	ClientIdentityTilde   = "~/.config/tainer/keys/tainer_rsa"
	DropInName            = "0-tainer.conf"
	UserConfigIncludeLine = "Include ~/.cyberstack/0-tainer.conf"
)

// ClientConfigContent returns the ssh_config stanza that points ssh.tainer.me
// at the tainer key only. IdentitiesOnly drops unrelated agent keys so a loaded
// agent can't exhaust the server's MaxAuthTries before the tainer key is tried.
func ClientConfigContent(identityPath string) string {
	return fmt.Sprintf("Host ssh.tainer.me\n  IdentityFile %s\n  IdentitiesOnly yes\n", identityPath)
}

// WriteDropIn writes the client config stanza to path (0644, world-readable so
// any user's ssh can read the system drop-in).
func WriteDropIn(path, identityPath string) error {
	return os.WriteFile(path, []byte(ClientConfigContent(identityPath)), 0644)
}
