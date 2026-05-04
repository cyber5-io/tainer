package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// EnsureKeyPair generates an Ed25519 SSH keypair if it doesn't exist.
func EnsureKeyPair(privPath, pubPath string) error {
	if _, err := os.Stat(privPath); err == nil {
		return nil // already exists
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating SSH key: %w", err)
	}

	// Marshal private key to PEM
	privBytes, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return fmt.Errorf("marshaling private key: %w", err)
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(privBytes), 0600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}

	// Marshal public key to authorized_keys format
	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return fmt.Errorf("converting public key: %w", err)
	}
	pubData := ssh.MarshalAuthorizedKey(sshPub)
	if err := os.WriteFile(pubPath, pubData, 0644); err != nil {
		return fmt.Errorf("writing public key: %w", err)
	}

	return nil
}

// EnsureHostKey generates an Ed25519 host key if it doesn't exist.
func EnsureHostKey(privPath string) error {
	if _, err := os.Stat(privPath); err == nil {
		return nil
	}
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating host key: %w", err)
	}
	privBytes, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return fmt.Errorf("marshaling host key: %w", err)
	}
	return os.WriteFile(privPath, pem.EncodeToMemory(privBytes), 0644)
}

// ReadPublicKey reads the public key from disk in authorized_keys format.
func ReadPublicKey(pubPath string) (string, error) {
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
