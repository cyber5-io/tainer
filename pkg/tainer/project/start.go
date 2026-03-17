package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/dns"
	"github.com/containers/podman/v6/pkg/tainer/identity"
	"github.com/containers/podman/v6/pkg/tainer/manifest"
	"github.com/containers/podman/v6/pkg/tainer/network"
	projRegistry "github.com/containers/podman/v6/pkg/tainer/registry"
	"github.com/containers/podman/v6/pkg/tainer/router"
	"github.com/containers/podman/v6/pkg/tainer/ssh"
	"github.com/containers/podman/v6/pkg/tainer/tls"

	"gopkg.in/yaml.v3"
)

type localState struct {
	Network struct {
		Subnet string `yaml:"subnet"`
		Name   string `yaml:"name"`
	} `yaml:"network"`
}

// Start executes the full tainer start flow for a project directory.
func Start(projectDir string) error {
	// 1. Read and validate manifest
	m, err := manifest.LoadFromDir(projectDir)
	if err != nil {
		return err
	}

	// 1b. Detect uid/gid for container injection
	uid, gid, err := identity.Detect(projectDir)
	if err != nil {
		return fmt.Errorf("detecting uid/gid: %w", err)
	}

	// 2. Ensure config dirs exist
	if err := config.EnsureDirs(); err != nil {
		return fmt.Errorf("creating config dirs: %w", err)
	}

	// 3. Check/update TLS cert
	certPath := config.CertFile()
	if tls.CertExists(certPath) {
		_, needsRenewal, err := tls.CheckExpiry(certPath)
		if err == nil && needsRenewal {
			fmt.Println("TLS certificate expires soon. Run 'tainer update' to renew.")
		}
	}

	// 4. Check offline DNS resolver
	if !dns.IsResolverInstalled() {
		fmt.Println("Setting up offline DNS resolver (requires sudo)...")
		if err := dns.InstallResolver(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not install DNS resolver: %v\n", err)
			fmt.Println("DNS will only work while online.")
		}
	}

	// 5. Ensure SSH keys exist
	if err := ssh.EnsureKeyPair(config.PrivateKey(), config.PublicKey()); err != nil {
		return fmt.Errorf("generating SSH keys: %w", err)
	}
	if err := ssh.EnsureHostKey(config.SSHPiperHostKey()); err != nil {
		return fmt.Errorf("generating sshpiper host key: %w", err)
	}

	// 6. Register project
	if err := projRegistry.Add(m.Project.Name, projectDir, string(m.Project.Type), m.Project.Domain); err != nil {
		return err
	}

	// 7. Allocate network
	subnet, err := network.AllocateSubnet(m.Project.Name)
	if err != nil {
		return err
	}
	netName := network.NetworkName(m.Project.Name)

	// Save local state
	ls := localState{}
	ls.Network.Subnet = subnet
	ls.Network.Name = netName
	lsData, _ := yaml.Marshal(ls)
	if err := os.WriteFile(filepath.Join(projectDir, ".tainer.local.yaml"), lsData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write .tainer.local.yaml: %v\n", err)
	}

	// 8. Create Podman network
	if err := network.CreateNetwork(netName, subnet); err != nil {
		return err
	}

	// 9. Build images
	fmt.Println("Building images...")
	if err := BuildImages(m); err != nil {
		return err
	}

	// 10. Create and start project pod
	podName := fmt.Sprintf("tainer-%s", m.Project.Name)
	if isPodRunning(podName) {
		fmt.Printf("%s is already running\n", m.Project.Name)
		return nil
	}
	if err := createProjectPod(m, podName, netName, projectDir, uid, gid); err != nil {
		return err
	}

	// 11. Start router
	if !router.IsRouterRunning() {
		router.WriteCaddyfile(config.CaddyfilePath(), nil, "/certs/tainer.me.crt", "/certs/tainer.me.key")
		if err := router.StartRouter(); err != nil {
			return fmt.Errorf("starting router: %w", err)
		}
	}

	// 12. Connect router to project network
	if err := router.ConnectToProjectNetwork(netName); err != nil {
		return fmt.Errorf("connecting router to project network: %w", err)
	}

	// 13. Update router config (Caddy + sshpiper)
	if err := updateRouterConfig(); err != nil {
		return fmt.Errorf("updating router config: %w", err)
	}

	// Add sshpiper entry
	projectIP := getProjectIP(podName)
	if err := router.AddSSHPiperEntry(config.SSHPiperDir(), m.Project.Name, projectIP, config.PrivateKey()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: sshpiper setup failed: %v\n", err)
	}

	// 14. Run post-deploy (idempotent)
	fmt.Println("Running post-deploy checks...")
	if err := runPostDeploy(m, podName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: post-deploy failed: %v\n", err)
		fmt.Println("Pod is still running — SSH in to debug.")
	}

	// 15. Output
	sshPort := router.SSHPort()
	portFlag := ""
	if sshPort != 22 {
		portFlag = fmt.Sprintf(" -p %d", sshPort)
	}
	fmt.Printf("\n%s started\n", m.Project.Name)
	fmt.Printf("  https://%s\n", m.Project.Domain)
	fmt.Printf("  ssh%s %s@ssh.tainer.me\n", portFlag, m.Project.Name)

	return nil
}

func createProjectPod(m *manifest.Manifest, podName, netName, projectDir string, uid, gid uint32) error {
	prefix := fmt.Sprintf("tainer-%s", m.Project.Name)

	// Create pod
	createArgs := []string{"pod", "create", "--name", podName, "--network", netName,
		"--add-host", fmt.Sprintf("%s:127.0.0.1", m.Project.Domain),
		"--label", fmt.Sprintf("tainer.project=%s", m.Project.Name),
		"--label", fmt.Sprintf("tainer.manifest=%s", filepath.Join(projectDir, manifest.FileName)),
		"--label", fmt.Sprintf("tainer.domain=%s", m.Project.Domain),
	}
	if output, err := exec.Command("tainer", createArgs...).CombinedOutput(); err != nil {
		if !strings.Contains(string(output), "already exists") {
			return fmt.Errorf("creating pod: %s", string(output))
		}
	}

	containerAppPath := m.ContainerAppPath()
	envFlags := identity.EnvFlags(uid, gid)

	// Inject SSH public key into authorized_keys
	pubKey, err := os.ReadFile(config.PublicKey())
	if err != nil {
		return fmt.Errorf("reading SSH public key %s: %w", config.PublicKey(), err)
	}
	authKeysPath := filepath.Join(projectDir, ".tainer-authorized_keys")
	if err := os.WriteFile(authKeysPath, pubKey, 0600); err != nil {
		return fmt.Errorf("writing authorized_keys: %w", err)
	}

	// Build common mount flags
	appMount := []string{"-v", filepath.Join(projectDir, m.HostAppDir()) + ":" + containerAppPath + ":rw"}
	mountBase := m.ContainerMountBase()
	dataMount := []string{"-v", filepath.Join(projectDir, "data") + ":" + mountBase + "/data:rw"}
	// Custom mounts (from tainer mount add)
	for _, name := range m.Mounts {
		dataMount = append(dataMount, "-v", filepath.Join(projectDir, name)+":"+mountBase+"/"+name+":rw")
	}
	authKeyMount := []string{"-v", authKeysPath + ":/home/tainer/.ssh/authorized_keys:ro"}
	certMount := []string{
		"-v", config.CertFile() + ":/certs/tainer.me.crt:ro",
		"-v", config.KeyFile() + ":/certs/tainer.me.key:ro",
	}
	envFile := []string{"--env-file", filepath.Join(projectDir, ".env")}

	// Start main container (caddy for PHP, node for Node.js)
	if m.IsPHP() {
		mainArgs := []string{"run", "-d", "--pod", podName, "--name", prefix + "-caddy-ct"}
		mainArgs = append(mainArgs, appMount...)
		mainArgs = append(mainArgs, dataMount...)
		mainArgs = append(mainArgs, authKeyMount...)
		mainArgs = append(mainArgs, certMount...)
		mainArgs = append(mainArgs, envFile...)
		mainArgs = append(mainArgs, envFlags...)
		mainArgs = append(mainArgs, prefix+"-caddy")
		if output, err := exec.Command("tainer", mainArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("starting main container: %s", string(output))
		}

		// PHP-FPM container (needs app + data mounts but not SSH/env-file)
		phpLimitsFlags := m.Runtime.Limits.EnvFlags()
		fpmArgs := []string{"run", "-d", "--pod", podName, "--name", prefix + "-phpfpm-ct"}
		fpmArgs = append(fpmArgs, appMount...)
		fpmArgs = append(fpmArgs, dataMount...)
		fpmArgs = append(fpmArgs, envFlags...)
		fpmArgs = append(fpmArgs, phpLimitsFlags...)
		fpmArgs = append(fpmArgs, prefix+"-phpfpm")
		if output, err := exec.Command("tainer", fpmArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("starting PHP-FPM: %s", string(output))
		}
	} else {
		mainArgs := []string{"run", "-d", "--pod", podName, "--name", prefix + "-node-ct"}
		mainArgs = append(mainArgs, appMount...)
		mainArgs = append(mainArgs, dataMount...)
		mainArgs = append(mainArgs, authKeyMount...)
		mainArgs = append(mainArgs, envFile...)
		mainArgs = append(mainArgs, envFlags...)
		mainArgs = append(mainArgs, prefix+"-node")
		if output, err := exec.Command("tainer", mainArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("starting Node container: %s", string(output))
		}
	}

	// Database container
	if m.HasDatabase() {
		dbMount := "/var/lib/mysql"
		if m.Runtime.Database == manifest.DatabasePostgres {
			dbMount = "/var/lib/postgresql/data"
		}
		dbDataDir := filepath.Join(projectDir, "db")
		dbArgs := []string{"run", "-d", "--pod", podName, "--name", prefix + "-db-ct",
			"-v", dbDataDir + ":" + dbMount + ":rw",
			"--env-file", filepath.Join(projectDir, ".env"),
		}
		dbArgs = append(dbArgs, envFlags...)
		dbArgs = append(dbArgs, prefix+"-db")
		if output, err := exec.Command("tainer", dbArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("starting database: %s", string(output))
		}
	}

	return nil
}

func updateRouterConfig() error {
	projects := projRegistry.All()
	var caddyProjects []router.CaddyProject
	for name, p := range projects {
		podName := fmt.Sprintf("tainer-%s", name)
		cmd := exec.Command("tainer", "pod", "inspect", "--format", "{{.State}}", podName)
		output, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "Running" {
			continue
		}
		ip := getProjectIP(podName)
		port := "80"
		pt := manifest.ProjectType(p.Type)
		if pt == manifest.TypeNodeJS || pt == manifest.TypeNextJS ||
			pt == manifest.TypeNuxtJS || pt == manifest.TypeKompozi {
			port = "3000"
		}
		caddyProjects = append(caddyProjects, router.CaddyProject{
			Domain: p.Domain,
			IP:     ip,
			Port:   port,
		})
	}
	if err := router.WriteCaddyfile(config.CaddyfilePath(), caddyProjects, "/certs/tainer.me.crt", "/certs/tainer.me.key"); err != nil {
		return err
	}
	return router.ReloadCaddy()
}

func mainContainerName(m *manifest.Manifest, podName string) string {
	if m.IsPHP() {
		return podName + "-caddy-ct"
	}
	return podName + "-node-ct"
}

func isPodRunning(podName string) bool {
	cmd := exec.Command("tainer", "pod", "inspect", "--format", "{{.State}}", podName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "Running"
}

func getProjectIP(podName string) string {
	cmd := exec.Command("tainer", "pod", "inspect", podName,
		"--format", "{{.InfraContainerID}}")
	infraID, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	ipCmd := exec.Command("tainer", "inspect", strings.TrimSpace(string(infraID)),
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}")
	output, err := ipCmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func runPostDeploy(m *manifest.Manifest, podName string) error {
	tmplDir := filepath.Join(config.TemplatesDir(), string(m.Project.Type))
	script := filepath.Join(tmplDir, "post-deploy.sh")
	if _, err := os.Stat(script); err != nil {
		return nil // no post-deploy script
	}

	ct := mainContainerName(m, podName)

	destPath := "/tmp/post-deploy.sh"
	cpCmd := exec.Command("tainer", "cp", script, ct+":"+destPath)
	if output, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copying post-deploy script: %s", string(output))
	}

	cmd := exec.Command("tainer", "exec", ct, "sh", destPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
