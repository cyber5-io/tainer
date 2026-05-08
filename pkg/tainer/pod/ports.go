package pod

// DerivePort returns the host TCP port for a (pod_id, role) pair,
// computed as `pod_id*10 + TCPServiceOffset(role)`. Returns 0 for
// unknown roles or roles outside the fixed table (the caller must
// then look up the user-pinned port from the registry).
//
// Pod 3012's db (offset 1) → 30121.
// Pod 3012's cache (offset 3) → 30123.
func DerivePort(podID int, role string) int {
	off := TCPServiceOffset(role)
	if off < 0 {
		return 0
	}
	return podID*10 + off
}
