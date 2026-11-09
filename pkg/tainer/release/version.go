package release

import (
	"strconv"
	"strings"
)

// Newer reports whether candidate is a strictly higher dotted-numeric
// version than installed. Dev builds never nag, and anything
// unparsable is "not newer".
func Newer(candidate, installed string) bool {
	if installed == "dev" {
		return false
	}
	c, ok := parseVersion(candidate)
	if !ok {
		return false
	}
	i, ok := parseVersion(installed)
	if !ok {
		return false
	}
	for n := 0; n < len(c) || n < len(i); n++ {
		var a, b int
		if n < len(c) {
			a = c[n]
		}
		if n < len(i) {
			b = i[n]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

func parseVersion(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for n, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return nil, false
		}
		out[n] = x
	}
	return out, true
}
