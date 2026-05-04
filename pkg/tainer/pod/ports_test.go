package pod

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

func TestDerivePortDB(t *testing.T) {
	ports := []manifest.PortEntry{
		{Role: "db", Container: 3306, Protocol: manifest.PortTCP},
	}
	got := DerivePort(7, ports, "db")
	if got != 30070 {
		t.Errorf("octet 7 db: got %d, want 30070", got)
	}
}

func TestDerivePortMultiTCP(t *testing.T) {
	ports := []manifest.PortEntry{
		{Role: "cache", Container: 6379, Protocol: manifest.PortTCP},
		{Role: "db", Container: 3306, Protocol: manifest.PortTCP},
	}
	// alphabetical: cache=index 0, db=index 1
	if got := DerivePort(7, ports, "cache"); got != 30070 {
		t.Errorf("octet 7 cache: got %d, want 30070", got)
	}
	if got := DerivePort(7, ports, "db"); got != 30071 {
		t.Errorf("octet 7 db: got %d, want 30071", got)
	}
}

func TestDerivePortIgnoresHTTP(t *testing.T) {
	ports := []manifest.PortEntry{
		{Role: "mail", Container: 8025, Protocol: manifest.PortHTTP}, // skipped
		{Role: "db", Container: 3306, Protocol: manifest.PortTCP},    // index 0
	}
	if got := DerivePort(7, ports, "db"); got != 30070 {
		t.Errorf("octet 7 db: got %d, want 30070", got)
	}
	if got := DerivePort(7, ports, "mail"); got != 0 {
		t.Errorf("HTTP role should derive 0, got %d", got)
	}
}

func TestDerivePortDifferentOctets(t *testing.T) {
	ports := []manifest.PortEntry{{Role: "db", Container: 3306, Protocol: manifest.PortTCP}}
	cases := []struct {
		octet, want int
	}{
		{7, 30070},
		{8, 30080},
		{9, 30090},
		{99, 30990},
	}
	for _, c := range cases {
		if got := DerivePort(c.octet, ports, "db"); got != c.want {
			t.Errorf("octet %d: got %d, want %d", c.octet, got, c.want)
		}
	}
}
