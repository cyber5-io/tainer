package config

import (
	"os"
	"path/filepath"
)

func BaseDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "tainer")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tainer")
}

func ProjectsFile() string  { return filepath.Join(BaseDir(), "projects.json") }
func NetworksFile() string  { return filepath.Join(BaseDir(), "networks.json") }
func KeysDir() string       { return filepath.Join(BaseDir(), "keys") }
func CertsDir() string      { return filepath.Join(BaseDir(), "certs") }
func TemplatesDir() string  { return filepath.Join(BaseDir(), "templates") }
func RouterDir() string     { return filepath.Join(BaseDir(), "router") }
func SSHPiperDir() string   { return filepath.Join(BaseDir(), "router", "sshpiper") }
func CaddyfilePath() string { return filepath.Join(RouterDir(), "Caddyfile") }
func DnsmasqConf() string   { return filepath.Join(RouterDir(), "dnsmasq.conf") }
func CertFile() string      { return filepath.Join(CertsDir(), "tainer.me.crt") }
func KeyFile() string       { return filepath.Join(CertsDir(), "tainer.me.key") }
func PrivateKey() string       { return filepath.Join(KeysDir(), "tainer_rsa") }
func PublicKey() string        { return filepath.Join(KeysDir(), "tainer_rsa.pub") }
func SSHPiperHostKey() string  { return filepath.Join(KeysDir(), "sshpiper_host_ed25519") }

func EnsureDirs() error {
	dirs := []string{
		BaseDir(),
		KeysDir(),
		CertsDir(),
		TemplatesDir(),
		RouterDir(),
		SSHPiperDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
