package router

import (
	"fmt"
	"os"
	"path/filepath"
)

// AddSSHPiperEntry creates a workingdir entry for sshpiper to route SSH by username.
// The workingdir plugin expects: sshpiper_upstream, authorized_keys, id_rsa per user directory.
func AddSSHPiperEntry(baseDir, projectName, projectIP, privateKeyPath string) error {
	projectDir := filepath.Join(baseDir, projectName)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return fmt.Errorf("creating sshpiper dir: %w", err)
	}

	// Copy the tainer private key into the sshpiper workingdir
	keyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("reading private key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "id_rsa"), keyData, 0644); err != nil {
		return fmt.Errorf("writing private key to sshpiper dir: %w", err)
	}

	// Create authorized_keys from user's SSH public keys
	homeDir, _ := os.UserHomeDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	var authorizedKeys []byte
	pubKeyFiles := []string{"id_rsa.pub", "id_ecdsa.pub", "id_ed25519.pub"}
	for _, f := range pubKeyFiles {
		data, err := os.ReadFile(filepath.Join(sshDir, f))
		if err == nil {
			authorizedKeys = append(authorizedKeys, data...)
			if len(data) > 0 && data[len(data)-1] != '\n' {
				authorizedKeys = append(authorizedKeys, '\n')
			}
		}
	}
	if len(authorizedKeys) > 0 {
		if err := os.WriteFile(filepath.Join(projectDir, "authorized_keys"), authorizedKeys, 0644); err != nil {
			return fmt.Errorf("writing authorized_keys: %w", err)
		}
	}

	// sshpiper_upstream: plain text file with format [user@]host[:port]
	upstream := fmt.Sprintf("tainer@%s:22\n", projectIP)
	if err := os.WriteFile(filepath.Join(projectDir, "sshpiper_upstream"), []byte(upstream), 0644); err != nil {
		return fmt.Errorf("writing sshpiper_upstream: %w", err)
	}

	return nil
}

// RemoveSSHPiperEntry removes the workingdir entry for a project.
func RemoveSSHPiperEntry(baseDir, projectName string) {
	os.RemoveAll(filepath.Join(baseDir, projectName))
}
