package release

import "testing"

func TestNewerComparesNumerically(t *testing.T) {
	cases := []struct {
		cand, inst string
		want       bool
	}{
		{"1.4.10", "1.4.9", true},
		{"1.5.0", "1.4.99", true},
		{"1.4.9", "1.4.10", false},
		{"1.4.0", "1.4.0", false},
		{"1.4.1", "dev", false},
		{"x.y", "1.4.0", false},
		{"1.4.1", "x.y", false},
		{"", "1.4.0", false},
		{"1.5", "1.4.9", true},
		{"1.4.0", "1.4", false},
	}
	for _, c := range cases {
		if got := Newer(c.cand, c.inst); got != c.want {
			t.Errorf("Newer(%q,%q)=%v want %v", c.cand, c.inst, got, c.want)
		}
	}
}
