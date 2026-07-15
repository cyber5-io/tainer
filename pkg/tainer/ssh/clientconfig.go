package ssh

import (
	"fmt"
	"os"
	"strings"
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
	if err := os.WriteFile(path, []byte(ClientConfigContent(identityPath)), 0644); err != nil {
		return err
	}
	// os.WriteFile only applies the mode on creation; enforce 0644 on
	// pre-existing files too so the system drop-in stays world-readable.
	return os.Chmod(path, 0644)
}

// EnsureUserConfigInclude guarantees includeLine is the first non-empty line of
// the ssh config at sshConfigPath, exactly once. If the file does not exist it
// is a no-op (the system drop-in covers that case). Preserves the file's mode
// and its newline style (LF or CRLF).
func EnsureUserConfigInclude(sshConfigPath, includeLine string) error {
	data, err := os.ReadFile(sshConfigPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", sshConfigPath, err)
	}

	content := string(data)
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	var kept []string
	for _, ln := range strings.Split(content, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) != includeLine {
			kept = append(kept, ln)
		}
	}
	// Drop leading empty lines so the include lands on line 1 cleanly.
	for len(kept) > 0 && strings.TrimSpace(kept[0]) == "" {
		kept = kept[1:]
	}
	rebuilt := includeLine + newline + strings.Join(kept, newline)

	if content == rebuilt {
		return nil
	}
	mode := os.FileMode(0600)
	if fi, statErr := os.Stat(sshConfigPath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(sshConfigPath, []byte(rebuilt), mode); err != nil {
		return fmt.Errorf("writing %s: %w", sshConfigPath, err)
	}
	return nil
}
