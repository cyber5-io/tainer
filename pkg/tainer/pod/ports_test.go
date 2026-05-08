package pod

import "testing"

func TestDerivePortAuto(t *testing.T) {
	cases := []struct {
		podID int
		role  string
		want  int
	}{
		{3012, RoleDB, 30121},
		{3012, RoleApp, 30122},
		{3012, RoleCache, 30123},
		{3007, RoleDB, 30071},
		{3999, RoleStorage, 39998},
	}
	for _, c := range cases {
		got := DerivePort(c.podID, c.role)
		if got != c.want {
			t.Errorf("DerivePort(%d, %q): got %d, want %d", c.podID, c.role, got, c.want)
		}
	}
}

func TestDerivePortHTTPRoleReturnsZero(t *testing.T) {
	if got := DerivePort(3012, RoleWeb); got != 0 {
		t.Errorf("web role should return 0 (no host port): got %d", got)
	}
}

func TestDerivePortUnknownRoleReturnsZero(t *testing.T) {
	if got := DerivePort(3012, "nonsense"); got != 0 {
		t.Errorf("unknown role should return 0: got %d", got)
	}
}
