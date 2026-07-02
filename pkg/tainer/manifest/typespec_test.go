package manifest

import "testing"

// TestTypeSpecTable locks the table's structural invariants so a new
// entry can't silently ship half-filled.
func TestTypeSpecTable(t *testing.T) {
	if len(typeSpecs) == 0 {
		t.Fatal("empty type spec table")
	}
	seen := map[ProjectType]bool{}
	for _, s := range typeSpecs {
		if seen[s.Type] {
			t.Errorf("%s: duplicate entry", s.Type)
		}
		seen[s.Type] = true

		switch s.Family {
		case FamilyPHP:
			if s.DefaultRuntime.PHP == "" {
				t.Errorf("%s: PHP family without a default PHP version", s.Type)
			}
			if s.DefaultRuntime.Node != "" {
				t.Errorf("%s: PHP family with a Node version", s.Type)
			}
		case FamilyNode:
			if s.DefaultRuntime.Node == "" {
				t.Errorf("%s: Node family without a default Node version", s.Type)
			}
			if s.DefaultRuntime.PHP != "" {
				t.Errorf("%s: Node family with a PHP version", s.Type)
			}
		default:
			t.Errorf("%s: unknown family %q", s.Type, s.Family)
		}

		if s.DefaultRuntime.Database == "" {
			t.Errorf("%s: no default database", s.Type)
		}
		// Exec identity comes as a matched pair: types with an app
		// role must define both, types without must define neither.
		if s.HasAppRole && (s.ExecUser == "" || s.ExecWorkdir == "") {
			t.Errorf("%s: app role without exec user/workdir", s.Type)
		}
		if !s.HasAppRole && (s.ExecUser != "" || s.ExecWorkdir != "") {
			t.Errorf("%s: exec user/workdir set but type has no app role", s.Type)
		}
	}
}

func TestCanonicalType(t *testing.T) {
	cases := []struct {
		in   string
		want ProjectType
		ok   bool
	}{
		{"wordpress", TypeWordPress, true},
		{"wp", TypeWordPress, true},
		{"WP", TypeWordPress, true},
		{"next", TypeNextJS, true},
		{"node", TypeNodeJS, true},
		{"kompozi", TypeKompozi, true},
		{"rails", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := CanonicalType(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("CanonicalType(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestToolTypes(t *testing.T) {
	if got := ToolTypes("wp"); len(got) != 1 || got[0] != TypeWordPress {
		t.Errorf("ToolTypes(wp) = %v, want [wordpress]", got)
	}
	if got := ToolTypes("composer"); len(got) != 2 {
		t.Errorf("ToolTypes(composer) = %v, want wordpress+php", got)
	}
	if got := ToolTypes("npm"); len(got) != 6 {
		t.Errorf("ToolTypes(npm) = %v, want the 6 node-flavoured types", got)
	}
	if got := ToolTypes("kubectl"); got != nil {
		t.Errorf("ToolTypes(kubectl) = %v, want nil", got)
	}
}
