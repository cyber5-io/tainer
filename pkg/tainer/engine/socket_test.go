package engine

import "testing"

func TestDefaultSocketPathPerOS(t *testing.T) {
	cases := []struct {
		goos, home, want string
	}{
		{"darwin", "/Users/leni", "/Users/leni/.cyberstack/cyberstack.sock"},
		{"linux", "/home/leni", "/run/cyberstack/cyberstack.sock"},
		{"linux", "", "/run/cyberstack/cyberstack.sock"},
		{"darwin", "", ""},
	}
	for _, c := range cases {
		if got := socketPathFor(c.goos, c.home); got != c.want {
			t.Errorf("socketPathFor(%q, %q) = %q, want %q", c.goos, c.home, got, c.want)
		}
	}
}
