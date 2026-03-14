package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/containers/podman/v6/pkg/tainer/config"
	"github.com/containers/podman/v6/pkg/tainer/dns"
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
	os.WriteFile(filepath.Join(projectDir, ".tainer.local.yaml"), lsData, 0644)

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
	if err := createProjectPod(m, podName, netName, projectDir); err != nil {
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
	router.AddSSHPiperEntry(config.SSHPiperDir(), m.Project.Name, projectIP, config.PrivateKey())

	// 14. Run post-deploy if first start
	if isFirstStart(m, podName) {
		fmt.Println("Running first-start setup...")
		if err := runPostDeploy(m, podName); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: post-deploy failed: %v\n", err)
			fmt.Println("Pod is still running — SSH in to debug.")
		}
		markInitialized(m, podName)
	}

	// 15. Output
	fmt.Printf("\n%s started\n", m.Project.Name)
	fmt.Printf("  https://%s\n", m.Project.Domain)
	fmt.Printf("  ssh -p 2222 %s@localhost\n", m.Project.Name)

	return nil
}

func createProjectPod(m *manifest.Manifest, podName, netName, projectDir string) error {
	prefix := fmt.Sprintf("tainer-%s", m.Project.Name)

	// Create pod
	createArgs := []string{"pod", "create", "--name", podName, "--network", netName,
		"--label", fmt.Sprintf("tainer.project=%s", m.Project.Name),
		"--label", fmt.Sprintf("tainer.manifest=%s", filepath.Join(projectDir, manifest.FileName)),
		"--label", fmt.Sprintf("tainer.domain=%s", m.Project.Domain),
	}
	if output, err := exec.Command("podman", createArgs...).CombinedOutput(); err != nil {
		if !strings.Contains(string(output), "already exists") {
			return fmt.Errorf("creating pod: %s", string(output))
		}
	}

	// Determine mount paths
	containerPath := "/var/www/html"
	if m.IsNode() {
		containerPath = "/app"
	}

	// UID mapping flag for Linux
	var usernsFlag []string
	if runtime.GOOS == "linux" {
		usernsFlag = []string{"--userns=keep-id"}
	}

	// Inject SSH public key into authorized_keys
	pubKey, _ := os.ReadFile(config.PublicKey())
	authKeysPath := filepath.Join(projectDir, ".tainer-authorized_keys")
	os.WriteFile(authKeysPath, pubKey, 0644)

	// Data volume for persistent project data
	dataVolume := fmt.Sprintf("tainer-%s-data", m.Project.Name)

	// Start main container
	if m.IsPHP() {
		dataMount := "/var/www/html/wp-content/uploads"
		if m.Project.Type == manifest.TypePHP {
			dataMount = "/var/www/html/data"
		}
		mainArgs := append([]string{"run", "-d", "--pod", podName,
			"--name", prefix + "-caddy-ct",
			"-v", projectDir + ":" + containerPath + ":rw",
			"-v", dataVolume + ":" + dataMount,
			"-v", authKeysPath + ":/root/.ssh/authorized_keys:ro",
			"--env-file", filepath.Join(projectDir, ".env"),
		}, usernsFlag...)
		mainArgs = append(mainArgs, prefix+"-caddy")
		if output, err := exec.Command("podman", mainArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("starting main container: %s", string(output))
		}

		// PHP-FPM container
		fpmArgs := append([]string{"run", "-d", "--pod", podName,
			"--name", prefix + "-phpfpm-ct",
			"-v", projectDir + ":" + containerPath + ":rw",
		}, usernsFlag...)
		fpmArgs = append(fpmArgs, prefix+"-phpfpm")
		if output, err := exec.Command("podman", fpmArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("starting PHP-FPM: %s", string(output))
		}
	} else {
		mainArgs := append([]string{"run", "-d", "--pod", podName,
			"--name", prefix + "-node-ct",
			"-v", projectDir + ":" + containerPath + ":rw",
			"-v", dataVolume + ":/app/data",
			"-v", authKeysPath + ":/root/.ssh/authorized_keys:ro",
			"--env-file", filepath.Join(projectDir, ".env"),
		}, usernsFlag...)
		mainArgs = append(mainArgs, prefix+"-node")
		if output, err := exec.Command("podman", mainArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("starting Node container: %s", string(output))
		}
	}

	// Database container
	if m.HasDatabase() {
		dbMount := "/var/lib/mysql"
		if m.Runtime.Database == manifest.DatabasePostgres {
			dbMount = "/var/lib/postgresql/data"
		}
		dbVolume := fmt.Sprintf("tainer-%s-db", m.Project.Name)
		dbArgs := []string{"run", "-d", "--pod", podName,
			"--name", prefix + "-db-ct",
			"-v", dbVolume + ":" + dbMount,
			"--env-file", filepath.Join(projectDir, ".env"),
			prefix + "-db",
		}
		if output, err := exec.Command("podman", dbArgs...).CombinedOutput(); err != nil {
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
		cmd := exec.Command("podman", "pod", "inspect", "--format", "{{.State}}", podName)
		output, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "Running" {
			continue
		}
		ip := getProjectIP(podName)
		caddyProjects = append(caddyProjects, router.CaddyProject{
			Domain: p.Domain,
			IP:     ip,
			Port:   "443",
		})
	}
	if err := router.WriteCaddyfile(config.CaddyfilePath(), caddyProjects, "/certs/tainer.me.crt", "/certs/tainer.me.key"); err != nil {
		return err
	}
	return router.ReloadCaddy(config.CaddyfilePath())
}

func mainContainerName(m *manifest.Manifest, podName string) string {
	if m.IsPHP() {
		return podName + "-caddy-ct"
	}
	return podName + "-node-ct"
}

func isPodRunning(podName string) bool {
	cmd := exec.Command("podman", "pod", "inspect", "--format", "{{.State}}", podName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "Running"
}

func getProjectIP(podName string) string {
	cmd := exec.Command("podman", "pod", "inspect", podName,
		"--format", "{{.InfraContainerID}}")
	infraID, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	ipCmd := exec.Command("podman", "inspect", strings.TrimSpace(string(infraID)),
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}")
	output, err := ipCmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func isFirstStart(m *manifest.Manifest, podName string) bool {
	ct := mainContainerName(m, podName)
	markerPath := markerFilePath(m)
	checkCmd := exec.Command("podman", "exec", ct, "test", "-f", markerPath)
	return checkCmd.Run() != nil
}

func markerFilePath(m *manifest.Manifest) string {
	if m.IsPHP() {
		return "/var/www/html/.tainer-initialized"
	}
	return "/app/data/.tainer-initialized"
}

func runPostDeploy(m *manifest.Manifest, podName string) error {
	tmplDir := filepath.Join(config.TemplatesDir(), string(m.Project.Type))
	script := filepath.Join(tmplDir, "post-deploy.sh")
	if _, err := os.Stat(script); err != nil {
		return nil // no post-deploy script
	}

	ct := mainContainerName(m, podName)

	destPath := "/tmp/post-deploy.sh"
	cpCmd := exec.Command("podman", "cp", script, ct+":"+destPath)
	if output, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copying post-deploy script: %s", string(output))
	}

	cmd := exec.Command("podman", "exec", ct, "sh", destPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func markInitialized(m *manifest.Manifest, podName string) {
	ct := mainContainerName(m, podName)
	markerPath := markerFilePath(m)
	exec.Command("podman", "exec", ct, "mkdir", "-p", filepath.Dir(markerPath)).Run()
	exec.Command("podman", "exec", ct, "touch", markerPath).Run()
}
