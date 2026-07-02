// Package runtime owns tainer's interaction with cyberstackd: locating
// the binary, probing the socket, spawning the daemon if absent, and
// returning a ready *engine.Client. Every CLI command starts by calling
// runtime.Engine() and proceeds from there — there is no other path
// between tainer and cyberstackd.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/network"
)

// Defaults that mirror cyberstackd's own conventions. Override via env
// when needed (DOCKER_HOST forces a specific socket path; CYBERSTACKD
// forces a specific binary path).
const (
	envDockerHost  = "DOCKER_HOST"
	envCyberstackd = "CYBERSTACKD"
	pidFile        = "cyberstackd.pid"
	// VM cold-boot — vfkit + Linux init + cs-agent startup — typically
	// finishes in 20–30s, occasionally longer on macOS with cold disk
	// caches. Set generously so the very first auto-spawn after a
	// pkill cyberstackd doesn't bail mid-boot.
	pingDeadline  = 60 * time.Second
	pingPollEvery = 250 * time.Millisecond
	// dnsPort is the embedded DNS responder port we tell cyberstackd to bind.
	dnsPort = 7753
)

// Options configures a runtime bring-up. Zero values are sensible
// defaults — most callers can pass Options{}.
type Options struct {
	// SocketPath overrides the cyberstack.sock location. If empty, the
	// engine package's default (~/.cyberstack/cyberstack.sock) is used.
	SocketPath string

	// CyberstackdBinary overrides the daemon binary path. If empty, $CYBERSTACKD
	// then $PATH lookup ("cyberstackd") is consulted. The installer will eventually
	// place this at /opt/tainer/bin/cyberstackd; until then we accept whatever
	// is on $PATH so devs can iterate against a freshly built daemon.
	CyberstackdBinary string

	// AutoStart, when true, spawns cyberstackd if no socket is reachable.
	// When false, Engine() errors instead of starting a daemon — useful
	// for diagnostic commands that should never side-effect.
	AutoStart bool

	// NetworkMode overrides the persisted mode at ~/.cyberstack/network-mode.
	// Empty means: read the persistence file; if absent, use network.DefaultMode.
	NetworkMode network.Mode
}

// Status describes whether cyberstackd is running and reachable.
type Status struct {
	Socket    string // socket the runtime is talking to
	Running   bool   // process is alive (PID resolvable)
	Reachable bool   // socket responds to /v1.x/_ping
	PID       int    // 0 when no PID file is present
}

// Engine returns a ready *engine.Client, starting cyberstackd via
// AutoStart=true if it isn't running yet. The caller owns Close().
//
// On AutoStart=false this is a pure probe — it never spawns.
func Engine(ctx context.Context, opts Options) (*engine.Client, error) {
	socket := resolveSocket(opts)

	// Fast path: existing socket, ping succeeds.
	if cli, err := dialAndPing(ctx, socket); err == nil {
		return cli, nil
	}

	if !opts.AutoStart {
		return nil, fmt.Errorf("cyberstackd not reachable at %s (autostart disabled)", socket)
	}

	if err := startDaemon(opts, socket); err != nil {
		return nil, err
	}

	// Spawning is async — poll until the socket answers or we time out.
	return waitForDaemon(ctx, socket)
}

// CurrentStatus probes without side-effects: does not start cyberstackd
// even if missing. Useful for `tainer status` and diagnostic surfaces.
func CurrentStatus(ctx context.Context, opts Options) Status {
	socket := resolveSocket(opts)
	st := Status{Socket: socket, PID: ReadPID(socket)}
	if st.PID > 0 {
		// On Unix, signal 0 is a no-op probe.
		if err := syscall.Kill(st.PID, 0); err == nil {
			st.Running = true
		}
	}
	if cli, err := dialAndPing(ctx, socket); err == nil {
		_ = cli.Close()
		st.Reachable = true
	}
	return st
}

func resolveSocket(opts Options) string {
	if opts.SocketPath != "" {
		return opts.SocketPath
	}
	if env := os.Getenv(envDockerHost); env != "" {
		return strings.TrimPrefix(env, "unix://")
	}
	return engine.DefaultSocketPath()
}

func dialAndPing(ctx context.Context, socket string) (*engine.Client, error) {
	if _, err := os.Stat(socket); err != nil {
		return nil, fmt.Errorf("socket missing: %w", err)
	}
	_ = os.Setenv(envDockerHost, "unix://"+socket)
	cli, err := engine.New()
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := cli.Ping(pingCtx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return cli, nil
}

// DaemonArgs builds the cyberstackd CLI flags from the runtime config.
// Exposed for testing and for the network-mode-switch flow that needs
// to spawn cyberstackd directly.
//
// -console-log is always on — captures the in-VM kernel + init + agent
// log to <socketDir>/console.log so panics inside the guest are
// recoverable. Tiny cost (file grows ~MB/min idle) for big debuggability.
func DaemonArgs(socket string, mode network.Mode, dnsPort int) []string {
	consoleLog := filepath.Join(filepath.Dir(socket), "console.log")
	return []string{
		"-socket", socket,
		"-dns-port", fmt.Sprintf("%d", dnsPort),
		"-network-mode", string(mode),
		"-console-log", consoleLog,
	}
}

func startDaemon(opts Options, socket string) error {
	bin, err := resolveBinary(opts)
	if err != nil {
		return err
	}

	mode := opts.NetworkMode
	if mode == "" {
		m, err := network.ReadMode(network.DefaultModeFile())
		if err != nil {
			return fmt.Errorf("read network-mode: %w", err)
		}
		mode = m
	}
	cmd := exec.Command(bin, DaemonArgs(socket, mode, dnsPort)...)

	// Detach: stdout/stderr to log file, no controlling terminal.
	logPath := filepath.Join(filepath.Dir(socket), "cyberstackd.log")
	rotateLog(logPath)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("spawn cyberstackd (%s): %w", bin, err)
	}

	// We deliberately don't Wait — cyberstackd is a long-running daemon.
	// Releasing the proc handle lets it outlive our process.
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release cyberstackd: %w", err)
	}
	return nil
}

// rotateLog caps cyberstackd.log growth at daemon spawn: past the
// threshold the current file becomes cyberstackd.log.old (replacing
// any previous .old) and a fresh one starts. One generation is enough
// history for post-mortems while bounding worst-case disk use to
// ~2× the threshold. Motivated by a real incident: weeks of
// reconnect-retry logging grew the file to 107 GB before anyone
// noticed. Best-effort — a rotation failure must never block the
// daemon from starting.
func rotateLog(path string) {
	const maxLogSize = 50 << 20 // 50 MB
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < maxLogSize {
		return
	}
	_ = os.Rename(path, path+".old")
}

func resolveBinary(opts Options) (string, error) {
	if opts.CyberstackdBinary != "" {
		return opts.CyberstackdBinary, nil
	}
	if env := os.Getenv(envCyberstackd); env != "" {
		return env, nil
	}
	if path, err := exec.LookPath("cyberstackd"); err == nil {
		return path, nil
	}
	// Final fallback: the installer's well-known location.
	const installed = "/opt/tainer/bin/cyberstackd"
	if _, err := os.Stat(installed); err == nil {
		return installed, nil
	}
	return "", errors.New("cannot locate cyberstackd: set $CYBERSTACKD or install tainer")
}

func waitForDaemon(ctx context.Context, socket string) (*engine.Client, error) {
	deadline := time.Now().Add(pingDeadline)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		cli, err := dialAndPing(ctx, socket)
		if err == nil {
			return cli, nil
		}
		lastErr = err
		time.Sleep(pingPollEvery)
	}
	if lastErr == nil {
		lastErr = errors.New("timeout")
	}
	return nil, fmt.Errorf("cyberstackd did not become reachable within %s: %w", pingDeadline, lastErr)
}

// ReadPID reads the PID written by cyberstackd into its PID file
// (adjacent to the socket at <socket-dir>/cyberstackd.pid). Returns 0
// if the file is absent or unparseable.
func ReadPID(socket string) int {
	path := filepath.Join(filepath.Dir(socket), pidFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}
