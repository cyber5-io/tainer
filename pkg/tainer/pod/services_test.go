package pod

import "testing"

func TestTCPServiceOffsetFixedTable(t *testing.T) {
	cases := []struct {
		role string
		want int
	}{
		{RoleDB, 1},
		{RoleApp, 2},
		{RoleCache, 3},
		{RoleMail, 4},
		{RoleSearch, 5},
		{RoleXdebug, 6},
		{RoleQueue, 7},
		{RoleStorage, 8},
		{RoleCustom, 9},
	}
	for _, tc := range cases {
		got := TCPServiceOffset(tc.role)
		if got != tc.want {
			t.Errorf("TCPServiceOffset(%q) = %d; want %d", tc.role, got, tc.want)
		}
	}
}

func TestTCPServiceOffsetUnknownRole(t *testing.T) {
	if got := TCPServiceOffset("nonsense"); got != -1 {
		t.Errorf("TCPServiceOffset(%q) = %d; want -1", "nonsense", got)
	}
}

func TestTCPServiceOffsetWebReserved(t *testing.T) {
	if got := TCPServiceOffset(RoleWeb); got != -1 {
		t.Errorf("TCPServiceOffset(%q) = %d; want -1 (web is HTTPS-only via caddy)", RoleWeb, got)
	}
}
