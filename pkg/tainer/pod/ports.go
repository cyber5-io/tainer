package pod

import (
	"sort"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// PortBase is the bottom of the auto-publish range. Spec §"Auto-port
// publish" — kept above all common dev defaults and below the macOS
// ephemeral range (49152+).
const PortBase = 30000

// PortPerOctet is the multiplier on the subnet octet. With 10 slots
// per project we cover 254 × 10 = 2,540 of the 10,000-port band.
const PortPerOctet = 10

// DerivePort returns the deterministic host port for a TCP service on
// (subnet octet, role) using the rule:
//     host_port = PortBase + (octet × PortPerOctet) + role_index
// where role_index is the alphabetical position of role among the
// manifest's TCP-protocol roles. HTTP roles always return 0 — they're
// not host-published per-pod (edge caddy handles them).
func DerivePort(octet int, ports []manifest.PortEntry, role string) int {
	tcpRoles := tcpRolesSorted(ports)
	for i, r := range tcpRoles {
		if r == role {
			return PortBase + octet*PortPerOctet + i
		}
	}
	return 0
}

// tcpRolesSorted returns the alphabetical list of role names whose
// manifest entry has protocol=tcp. Determinism guarantees the role
// index doesn't shuffle if the user reorders ports in the YAML.
func tcpRolesSorted(ports []manifest.PortEntry) []string {
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		if p.Protocol == manifest.PortTCP {
			out = append(out, p.Role)
		}
	}
	sort.Strings(out)
	return out
}
