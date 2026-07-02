// Package doctor walks the full tainer stack layer by layer — daemon,
// engine, network plumbing, router, TLS, DNS, pods — and reports the
// health of each. With Fix enabled it also applies the safe subset of
// recoveries (start the daemon, re-ensure the router) so `tainer
// doctor --fix` is the one command to run when "the website is dead".
//
// Check order is deliberate: each layer depends on the previous one,
// so the first failure in the list is almost always the root cause.
// Checks that can't run because an earlier layer is down report
// StatusSkip rather than piling on misleading failures.
package doctor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/config"
	"github.com/cyber5-io/tainer/pkg/tainer/dns"
	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/network"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/router"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
	tainertls "github.com/cyber5-io/tainer/pkg/tainer/tls"
)

// Status is the outcome of one check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn" // degraded but functional, or advisory
	StatusFail Status = "fail"
	StatusSkip Status = "skip" // prerequisite layer is down
)

// Result is one check's outcome. Fixed is set when Options.Fix was on
// and this check successfully applied a recovery (the Status then
// reflects the post-fix state).
type Result struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"` // suggested action when not OK
	Fixed  bool   `json:"fixed,omitempty"`
}

// Report is the full doctor run.
type Report struct {
	Results []Result `json:"checks"`
	// StoppedPods counts pods that exist but aren't running. Doctor
	// only probes running pods — stopped is a valid state, and
	// `tainer list` is the enumeration surface — so these appear only
	// as this count.
	StoppedPods int `json:"stoppedPods,omitempty"`
}

// Healthy reports whether the stack is fully functional: no fails,
// no skips (a skip means something upstream failed).
func (r Report) Healthy() bool {
	for _, res := range r.Results {
		if res.Status == StatusFail || res.Status == StatusSkip {
			return false
		}
	}
	return true
}

// Counts returns (ok, warn, fail, skip) totals for summary lines.
func (r Report) Counts() (ok, warn, fail, skip int) {
	for _, res := range r.Results {
		switch res.Status {
		case StatusOK:
			ok++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		case StatusSkip:
			skip++
		}
	}
	return
}

// Options configures a doctor run.
type Options struct {
	// Fix applies safe recoveries: start cyberstackd when it's down,
	// re-ensure the router containers. It never touches pod state or
	// anything requiring sudo — those stay as hints.
	Fix bool

	// HTTPTimeout bounds each per-pod HTTP probe. Zero means 5s.
	HTTPTimeout time.Duration

	// Progress, when non-nil, is called with each Result as it
	// completes so the caller can render incrementally. The same
	// results are also collected into the returned Report.
	Progress func(Result)

	// Started, when non-nil, is called with a check's name right
	// before it begins. Callers use it to show a spinner during the
	// slow checks (VM cold-boot under Fix, HTTP probes against dead
	// pods); the matching Progress call signals completion. Calls are
	// strictly sequential: every Started is followed by at least one
	// Progress before the next Started.
	Started func(name string)
}

// Run executes all checks in dependency order and returns the report.
func Run(ctx context.Context, opts Options) Report {
	if opts.HTTPTimeout <= 0 {
		opts.HTTPTimeout = 5 * time.Second
	}
	d := &run{opts: opts}

	eng := d.checkDaemon(ctx)
	if eng != nil {
		defer eng.Close()
	}
	d.checkAgent(ctx, eng)
	d.checkNetworkMode()
	d.checkRouter(ctx, eng)
	d.checkTLS()
	d.checkDNS(ctx)
	d.checkPods(ctx, eng)

	return d.report
}

type run struct {
	opts   Options
	report Report
	// agentUp records whether the VM/agent answered — pod checks skip
	// when it didn't, even if the daemon socket itself is alive.
	agentUp bool
}

func (d *run) add(r Result) {
	d.report.Results = append(d.report.Results, r)
	if d.opts.Progress != nil {
		d.opts.Progress(r)
	}
}

func (d *run) start(name string) {
	if d.opts.Started != nil {
		d.opts.Started(name)
	}
}

// checkDaemon covers the first two layers: the cyberstackd process
// and its Docker-API socket. Returns a connected engine client when
// the socket answers (possibly after a --fix start), nil otherwise.
func (d *run) checkDaemon(ctx context.Context) *engine.Client {
	d.start("daemon")
	st := runtime.CurrentStatus(ctx, runtime.Options{})

	if st.Reachable {
		detail := st.Socket
		if st.PID > 0 {
			detail = fmt.Sprintf("pid %d · %s", st.PID, st.Socket)
		}
		d.add(Result{Name: "daemon", Status: StatusOK, Detail: detail})
	} else if d.opts.Fix {
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			d.add(Result{Name: "daemon", Status: StatusFail,
				Detail: "not reachable and auto-start failed: " + err.Error(),
				Hint:   "check `cyberstackd` is installed at /opt/tainer/bin or on $PATH"})
			return nil
		}
		d.add(Result{Name: "daemon", Status: StatusOK, Detail: "was down — started", Fixed: true})
		return eng
	} else {
		detail := "not reachable at " + st.Socket
		if st.Running {
			// Process alive but socket dead — the historic vsock-wedge
			// signature. Worth calling out distinctly.
			detail = fmt.Sprintf("process alive (pid %d) but socket not answering", st.PID)
		}
		d.add(Result{Name: "daemon", Status: StatusFail, Detail: detail,
			Hint: "run `tainer doctor --fix` (or any `tainer start`) to bring it up"})
		return nil
	}

	eng, err := engine.New()
	if err != nil {
		return nil
	}
	return eng
}

// checkAgent proves the whole daemon→VM→agent chain works by asking
// for the container list — that call round-trips through the vsock
// gRPC channel into the in-guest agent.
func (d *run) checkAgent(ctx context.Context, eng *engine.Client) {
	d.start("agent")
	if eng == nil {
		d.add(Result{Name: "agent", Status: StatusSkip, Detail: "daemon is down"})
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := pod.List(cctx, eng); err != nil {
		d.add(Result{Name: "agent", Status: StatusFail,
			Detail: "daemon up but the container runtime isn't answering: " + err.Error(),
			Hint:   "restart the stack: `pkill cyberstackd` then `tainer start`"})
		return
	}
	d.agentUp = true
	d.add(Result{Name: "agent", Status: StatusOK, Detail: "container runtime responding"})
}

// checkNetworkMode reports the persisted mode and verifies its helper
// processes are alive. Also flags the known-bad combination of perf
// mode + a VPN network extension (which black-holes the in-process
// NAT path).
func (d *run) checkNetworkMode() {
	d.start("network")
	mode, err := network.ReadMode(network.DefaultModeFile())
	if err != nil {
		d.add(Result{Name: "network", Status: StatusWarn,
			Detail: "mode file unreadable, daemon uses default: " + err.Error()})
		return
	}

	needsGvproxy := mode == network.ModeCompatibility || mode == network.ModeHybrid
	if needsGvproxy && !processRunning("gvproxy") {
		d.add(Result{Name: "network", Status: StatusFail,
			Detail: mode.Display() + " — gvproxy process not running",
			Hint:   "restart the stack: `pkill cyberstackd` then `tainer start`"})
		return
	}

	if mode == network.ModePerformance && vpnActive() {
		d.add(Result{Name: "network", Status: StatusWarn,
			Detail: mode.Display() + " — VPN detected; performance mode's NAT breaks under VPN network extensions",
			Hint:   "switch with `tainer network mode set hybrid`"})
		return
	}

	d.add(Result{Name: "network", Status: StatusOK, Detail: mode.Display()})
}

// checkRouter verifies both router containers are running; with Fix
// it re-ensures them (create-or-start, config rewrite, caddy reload).
func (d *run) checkRouter(ctx context.Context, eng *engine.Client) {
	d.start("router")
	if eng == nil || !d.agentUp {
		d.add(Result{Name: "router", Status: StatusSkip, Detail: "daemon or agent is down"})
		return
	}
	down := routersDown(ctx, eng)
	if len(down) == 0 {
		d.add(Result{Name: "router", Status: StatusOK, Detail: "web + ssh running"})
		return
	}
	if d.opts.Fix {
		if err := router.Ensure(ctx, eng, nil); err != nil {
			d.add(Result{Name: "router", Status: StatusFail,
				Detail: strings.Join(down, ", ") + " down; ensure failed: " + err.Error()})
			return
		}
		if remaining := routersDown(ctx, eng); len(remaining) > 0 {
			d.add(Result{Name: "router", Status: StatusFail,
				Detail: strings.Join(remaining, ", ") + " still down after ensure"})
			return
		}
		d.add(Result{Name: "router", Status: StatusOK,
			Detail: strings.Join(down, ", ") + " was down — restarted", Fixed: true})
		return
	}
	d.add(Result{Name: "router", Status: StatusFail,
		Detail: strings.Join(down, ", ") + " not running",
		Hint:   "run `tainer doctor --fix` to restart the router"})
}

func routersDown(ctx context.Context, eng *engine.Client) []string {
	var down []string
	for _, name := range []string{router.WebContainerName, router.SSHContainerName} {
		insp, err := eng.Inspect(ctx, name)
		if err != nil || insp.State == nil || !insp.State.Running {
			down = append(down, name)
		}
	}
	return down
}

// checkTLS verifies the wildcard cert exists and isn't expired or
// about to. No auto-fix — cert distribution is an install-time
// concern and re-downloading needs the release URL wiring.
func (d *run) checkTLS() {
	d.start("tls")
	certPath := config.CertFile()
	if !tainertls.CertExists(certPath) {
		d.add(Result{Name: "tls", Status: StatusFail,
			Detail: "certificate missing: " + certPath,
			Hint:   "reinstall tainer to restore the *.tainer.me certificate"})
		return
	}
	expiry, needsRenewal, err := tainertls.CheckExpiry(certPath)
	if err != nil {
		d.add(Result{Name: "tls", Status: StatusFail, Detail: "certificate unreadable: " + err.Error()})
		return
	}
	if time.Now().After(expiry) {
		d.add(Result{Name: "tls", Status: StatusFail,
			Detail: "certificate expired " + expiry.Format("2006-01-02"),
			Hint:   "update tainer to get the renewed certificate"})
		return
	}
	if needsRenewal {
		d.add(Result{Name: "tls", Status: StatusWarn,
			Detail: "certificate expires " + expiry.Format("2006-01-02"),
			Hint:   "update tainer soon to get the renewed certificate"})
		return
	}
	d.add(Result{Name: "tls", Status: StatusOK, Detail: "valid until " + expiry.Format("2006-01-02")})
}

// checkDNS verifies the OS resolver hook is installed AND that a live
// query against the embedded responder answers 127.0.0.1. The two can
// diverge: the file can exist while the daemon (and its DNS listener)
// is down, or vice versa after a half-finished install.
func (d *run) checkDNS(ctx context.Context) {
	d.start("dns")
	installed := dns.IsResolverInstalled(dnsPort)
	addr, qerr := queryLocalDNS(ctx, "doctor-probe.tainer.me")

	switch {
	case installed && qerr == nil && addr == "127.0.0.1":
		d.add(Result{Name: "dns", Status: StatusOK, Detail: "*.tainer.me → 127.0.0.1"})
	case installed && qerr != nil:
		d.add(Result{Name: "dns", Status: StatusFail,
			Detail: "resolver installed but the local DNS responder isn't answering: " + qerr.Error(),
			Hint:   "the daemon serves DNS — bring it up with `tainer doctor --fix`"})
	case installed:
		d.add(Result{Name: "dns", Status: StatusFail,
			Detail: "local DNS answered " + addr + " (expected 127.0.0.1)"})
	default:
		d.add(Result{Name: "dns", Status: StatusFail,
			Detail: "OS resolver hook not installed for tainer.me",
			Hint:   "run `tainer start` from a project — it installs the resolver (asks for sudo)"})
	}
}

// checkPods probes each RUNNING pod's public URL end to end: DNS
// name, router TLS, reverse proxy, pod web container. Stopped pods
// aren't listed — stopped is a valid state and `tainer list` is the
// enumeration surface — they only feed Report.StoppedPods. A "mixed"
// pod (some containers up, some down) is genuinely unhealthy and
// still reports.
func (d *run) checkPods(ctx context.Context, eng *engine.Client) {
	if eng == nil || !d.agentUp {
		d.add(Result{Name: "pods", Status: StatusSkip, Detail: "daemon or agent is down"})
		return
	}
	pods, err := pod.List(ctx, eng)
	if err != nil {
		d.add(Result{Name: "pods", Status: StatusFail, Detail: err.Error()})
		return
	}
	probed := 0
	for _, p := range pods {
		name := "pod " + p.Name
		if p.State() == pod.StateStopped {
			d.report.StoppedPods++
			continue
		}
		probed++
		d.start(name)
		if p.State() == pod.StateMixed {
			d.add(Result{Name: name, Status: StatusFail,
				Detail: "some containers are down",
				Hint:   "try `tainer stop && tainer start` for this project"})
			continue
		}
		domain := podDomain(ctx, eng, p)
		if domain == "" {
			d.add(Result{Name: name, Status: StatusWarn,
				Detail: "running, but its manifest can't be located to find the domain"})
			continue
		}
		code, err := d.probeHTTPS(ctx, domain)
		// A pod that started seconds ago 502s briefly while PHP/node
		// boots behind the router. One short-fuse retry keeps doctor
		// from flagging a healthy-but-warming pod.
		if err == nil && (code == 502 || code == 503) {
			time.Sleep(2 * time.Second)
			code, err = d.probeHTTPS(ctx, domain)
		}
		switch {
		case err != nil:
			d.add(Result{Name: name, Status: StatusFail,
				Detail: fmt.Sprintf("https://%s unreachable: %v", domain, err),
				Hint:   "try `tainer stop && tainer start` for this project"})
		case code >= 500:
			d.add(Result{Name: name, Status: StatusFail,
				Detail: fmt.Sprintf("https://%s → HTTP %d", domain, code),
				Hint:   "the app is up but erroring — check its logs"})
		default:
			d.add(Result{Name: name, Status: StatusOK,
				Detail: fmt.Sprintf("https://%s → HTTP %d", domain, code)})
		}
	}
	if probed == 0 {
		detail := "no pods"
		if d.report.StoppedPods > 0 {
			detail = fmt.Sprintf("no running pods (%d stopped)", d.report.StoppedPods)
		}
		d.add(Result{Name: "pods", Status: StatusOK, Detail: detail})
	}
}

// podDomain resolves a pod's public domain via its manifest-path
// container label, mirroring what `tainer status` does.
func podDomain(ctx context.Context, eng *engine.Client, p pod.Pod) string {
	for _, c := range p.Containers {
		insp, err := eng.Inspect(ctx, c.Name)
		if err == nil && insp.Config != nil {
			if mp := insp.Config.Labels[pod.LabelManifestPath]; mp != "" {
				if m, merr := manifest.Load(mp); merr == nil {
					return m.Project.Domain
				}
			}
		}
	}
	return ""
}

// probeHTTPS GETs https://domain pinned to 127.0.0.1:443 so the probe
// tests the router path even if system DNS is broken (DNS gets its
// own check). The bundled *.tainer.me cert isn't in the system trust
// store, so verification is skipped — we're probing reachability and
// status codes, not trust.
func (d *run) probeHTTPS(ctx context.Context, domain string) (int, error) {
	dialer := &net.Dialer{Timeout: d.opts.HTTPTimeout}
	client := &http.Client{
		Timeout: d.opts.HTTPTimeout,
		Transport: &http.Transport{
			DialContext: func(dctx context.Context, netw, _ string) (net.Conn, error) {
				return dialer.DialContext(dctx, netw, "127.0.0.1:443")
			},
			TLSClientConfig: &tls.Config{ServerName: domain, InsecureSkipVerify: true},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// dnsPort mirrors the runtime package's embedded-DNS port. Kept as a
// local constant because runtime doesn't export it; if the daemon
// flag ever changes both must move together (grep: 7753).
const dnsPort = 7753

// queryLocalDNS asks the embedded responder directly on
// 127.0.0.1:7753, bypassing the OS resolver order, so the check
// isolates "is tainer's DNS answering" from "is the OS hook set up".
func queryLocalDNS(ctx context.Context, host string) (string, error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(dctx context.Context, netw, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(dctx, netw, fmt.Sprintf("127.0.0.1:%d", dnsPort))
		},
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := r.LookupHost(cctx, host)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no answer")
	}
	return addrs[0], nil
}

// processRunning reports whether a process whose command line matches
// pattern is alive. pgrep -f matches the full command line, which is
// what we need for helpers spawned with absolute paths.
func processRunning(pattern string) bool {
	return exec.Command("pgrep", "-f", pattern).Run() == nil
}

// vpnActive detects VPN clients whose packet-filter/network-extension
// hooks are known to break the in-process NAT path. Deliberately
// conservative: only names we've verified cause trouble.
func vpnActive() bool {
	return processRunning("NordVPN")
}
