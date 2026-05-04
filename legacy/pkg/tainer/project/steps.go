package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/dns"
	"github.com/containers/podman/v6/pkg/tainer/identity"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/network"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/router"
	"github.com/containers/podman/v6/pkg/tainer/ssh"
	"github.com/containers/podman/v6/pkg/tainer/tls"
	"github.com/containers/podman/v6/pkg/tainer/tui/progress"

	"gopkg.in/yaml.v3"
)

// StartInfo holds the information produced by Start that the caller needs.
type StartInfo struct {
	Name   string
	Domain string
	SSHCmd string
}

// StartSteps returns the progress steps for starting a project, plus info for the footer.
func StartSteps(projectDir string) ([]progress.Step, *StartInfo, error) {
	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return nil, nil, err
	}

	info := &StartInfo{
		Name:   m.Project.Name,
		Domain: m.Project.Domain,
	}

	// Shared state across steps
	var uid, gid uint32
	podName := fmt.Sprintf("tainer-%s", m.Project.Name)
	netName := network.NetworkName(m.Project.Name)
	var skipCreate bool // true if pod exists with same hash and just needs restart

	steps := []progress.Step{
		{
			Label: "Preparing environment",
			Run: func() error {
				restoreMissingConfigsSilent(m.Project.Name, projectDir)

				var err error
				uid, gid, err = identity.Detect(projectDir)
				if err != nil {
					return fmt.Errorf("detecting uid/gid: %w", err)
				}

				if err := config.EnsureDirs(); err != nil {
					return fmt.Errorf("creating config dirs: %w", err)
				}

				certPath := config.CertFile()
				if tls.CertExists(certPath) {
					// just check, don't print
					tls.CheckExpiry(certPath)
				}

				if !dns.IsResolverInstalled() {
					dns.InstallResolver()
				}

				if err := ssh.EnsureKeyPair(config.PrivateKey(), config.PublicKey()); err != nil {
					return fmt.Errorf("generating SSH keys: %w", err)
				}
				if err := ssh.EnsureHostKey(config.SSHPiperHostKey()); err != nil {
					return fmt.Errorf("generating sshpiper host key: %w", err)
				}

				return projRegistry.Add(m.Project.Name, projectDir, string(m.Project.Type), m.Project.Domain)
			},
		},
		{
			Label: "Setting up network",
			Run: func() error {
				subnet, err := network.AllocateSubnet(m.Project.Name)
				if err != nil {
					return err
				}

				ls := localState{}
				if data, err := os.ReadFile(filepath.Join(projectDir, ".tainer.local.yaml")); err == nil {
					yaml.Unmarshal(data, &ls)
				}
				ls.Network.Subnet = subnet
				ls.Network.Name = netName
				lsData, _ := yaml.Marshal(ls)
				os.WriteFile(filepath.Join(projectDir, ".tainer.local.yaml"), lsData, 0644)

				return network.CreateNetwork(netName, subnet)
			},
		},
		{
			Label: "Checking containers",
			Run: func() error {
				if IsPodRunning(podName) {
					skipCreate = true
					return nil
				}

				currentHash := manifestHash(projectDir)
				if podExists(podName) {
					storedManifest := getPodLabel(podName, "tainer.manifest-hash")
					if storedManifest != currentHash {
						exec.Command("tainer", "pod", "rm", "-f", podName).CombinedOutput()
					} else {
						// Same config — just restart
						if output, err := exec.Command("tainer", "pod", "start", podName).CombinedOutput(); err != nil {
							return fmt.Errorf("starting pod: %s", string(output))
						}
						skipCreate = true
						return nil
					}
				}

				// Pull images if config changed
				ls := localState{}
				if data, err := os.ReadFile(filepath.Join(projectDir, ".tainer.local.yaml")); err == nil {
					yaml.Unmarshal(data, &ls)
				}
				if ls.ManifestHash != currentHash {
					if err := PullImages(m); err != nil {
						return err
					}
					ls.ManifestHash = currentHash
					lsData, _ := yaml.Marshal(ls)
					os.WriteFile(filepath.Join(projectDir, ".tainer.local.yaml"), lsData, 0644)
				}

				return nil
			},
		},
		{
			Label: "Starting containers",
			Run: func() error {
				if skipCreate {
					return nil
				}
				return createProjectPod(m, podName, netName, projectDir, uid, gid)
			},
		},
		{
			Label: "Configuring router",
			Run: func() error {
				if !router.IsRouterRunning() {
					router.WriteCaddyfile(config.CaddyfilePath(), nil, "/certs/tainer.me.crt", "/certs/tainer.me.key")
					if err := router.StartRouter(); err != nil {
						return fmt.Errorf("starting router: %w", err)
					}
				}

				if err := router.ConnectToProjectNetwork(netName); err != nil {
					return fmt.Errorf("connecting router: %w", err)
				}

				if err := updateRouterConfig(); err != nil {
					return fmt.Errorf("updating router config: %w", err)
				}

				projectIP := getProjectIP(podName)
				router.AddSSHPiperEntry(config.SSHPiperDir(), m.Project.Name, projectIP, config.PrivateKey())

				return nil
			},
		},
		{
			Label: "Running post-deploy",
			Run: func() error {
				return runPostDeploy(m, podName)
			},
		},
	}

	// Build SSH command for footer
	sshPort := router.SSHPort()
	portFlag := ""
	if sshPort != 22 {
		portFlag = fmt.Sprintf(" -p %d", sshPort)
	}
	info.SSHCmd = fmt.Sprintf("ssh%s %s@ssh.tainer.me", portFlag, m.Project.Name)

	// Auto-backup after all steps succeed (called by the TUI after completion)
	steps = append(steps, progress.Step{
		Label: "Saving configuration",
		Run: func() error {
			config.Backup(m.Project.Name, projectDir)
			// Auto-open browser if configured
			if m.Project.AutoOpen != nil && *m.Project.AutoOpen {
				openURL(fmt.Sprintf("https://%s", m.Project.Domain))
			}
			return nil
		},
	})

	return steps, info, nil
}

// StopSteps returns the progress steps for stopping a project.
func StopSteps(projectName string) ([]progress.Step, error) {
	_, ok := projRegistry.Get(projectName)
	if !ok {
		return nil, fmt.Errorf("project %q not found in registry", projectName)
	}

	podName := fmt.Sprintf("tainer-%s", projectName)
	netName := network.NetworkName(projectName)

	steps := []progress.Step{
		{
			Label: "Stopping containers",
			Run: func() error {
				exec.Command("tainer", "pod", "stop", podName).CombinedOutput()
				exec.Command("tainer", "pod", "rm", "-f", podName).CombinedOutput()
				return nil
			},
		},
		{
			Label: "Updating router",
			Run: func() error {
				router.RemoveSSHPiperEntry(config.SSHPiperDir(), projectName)
				router.DisconnectFromProjectNetwork(netName)
				updateRouterConfig()
				if router.RunningProjectCount() == 0 {
					router.StopRouter()
				}
				return nil
			},
		},
	}

	return steps, nil
}

// DestroySteps returns the progress steps for destroying a project.
func DestroySteps(projectDir string) ([]progress.Step, string, error) {
	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return nil, "", err
	}

	podName := fmt.Sprintf("tainer-%s", m.Project.Name)
	netName := network.NetworkName(m.Project.Name)
	name := m.Project.Name

	steps := []progress.Step{
		{
			Label: "Stopping containers",
			Run: func() error {
				exec.Command("tainer", "pod", "stop", podName).CombinedOutput()
				exec.Command("tainer", "pod", "rm", "-f", podName).CombinedOutput()
				exec.Command("tainer", "volume", "prune", "-f").CombinedOutput()
				return nil
			},
		},
		{
			Label: "Removing network",
			Run: func() error {
				if router.IsRouterRunning() {
					router.DisconnectFromProjectNetwork(netName)
				}
				network.RemoveNetwork(netName)
				return nil
			},
		},
		{
			Label: "Cleaning up",
			Run: func() error {
				router.RemoveSSHPiperEntry(config.SSHPiperDir(), name)
				projRegistry.Remove(name)
				network.FreeSubnet(name)

				if router.IsRouterRunning() {
					if router.RunningProjectCount() == 0 {
						router.StopRouter()
					} else {
						updateRouterConfig()
					}
				}

				// Clean local state files
				os.Remove(filepath.Join(projectDir, ".tainer.local.yaml"))
				os.Remove(filepath.Join(projectDir, ".tainer-authorized_keys"))
				return nil
			},
		},
	}

	return steps, name, nil
}

// DestroyNukeStep returns an additional step that nukes all project files.
func DestroyNukeStep(projectDir string) progress.Step {
	return progress.Step{
		Label: "Removing project files",
		Run: func() error {
			entries, err := os.ReadDir(projectDir)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				os.RemoveAll(filepath.Join(projectDir, entry.Name()))
			}
			return nil
		},
	}
}

// restoreMissingConfigsSilent is like restoreMissingConfigs but doesn't prompt.
// In TUI mode we can't use stdin prompts.
func restoreMissingConfigsSilent(projectName, projectDir string) {
	if !config.BackupExists(projectName) {
		return
	}
	missing := config.MissingWithBackup(projectName, projectDir)
	if len(missing) == 0 {
		return
	}
	// Auto-restore without prompting in TUI mode
	config.RestoreFiles(projectName, projectDir, missing)
}

