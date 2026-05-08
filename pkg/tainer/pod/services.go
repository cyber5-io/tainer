package pod

// TCPServiceOffset returns the fixed-table offset for a TCP service role.
// The host port is then computed as `pod_id*10 + offset`, so for pod 3012
// the db role maps to 30121, cache to 30123, etc.
//
// Offset 0 is intentionally reserved (web is HTTPS-only — its routing goes
// through the edge caddy, not a host TCP port). Roles not in the fixed
// table return -1; the caller must use an explicit `host_port` from the
// manifest in that case.
func TCPServiceOffset(role string) int {
	switch role {
	case RoleDB:
		return 1
	case RoleApp:
		return 2
	case RoleCache:
		return 3
	case RoleMail:
		return 4
	case RoleSearch:
		return 5
	case RoleXdebug:
		return 6
	case RoleQueue:
		return 7
	case RoleStorage:
		return 8
	case RoleCustom:
		return 9
	}
	return -1
}
