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
		{"sveltekit", TypeSvelteKit, true},
		{"kit", TypeSvelteKit, true},
		{"KIT", TypeSvelteKit, true},
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
	if got := ToolTypes("npm"); len(got) != 8 {
		t.Errorf("ToolTypes(npm) = %v, want the 8 node-flavoured types", got)
	}
	if got := ToolTypes("kubectl"); got != nil {
		t.Errorf("ToolTypes(kubectl) = %v, want nil", got)
	}
}

// TestReactSpec locks TAIN-98: react pods carry an app role (sshd +
// node tooling) like every other node type — the SPA distinction
// lives in the caddy site config, not in the role set.
func TestReactSpec(t *testing.T) {
	s, ok := SpecFor(TypeReact)
	if !ok {
		t.Fatal("no spec for react")
	}
	if !s.HasAppRole {
		t.Error("react: HasAppRole = false, want true")
	}
	if s.ExecUser != "1000:1000" {
		t.Errorf("react: ExecUser = %q, want 1000:1000", s.ExecUser)
	}
	if s.ExecWorkdir != "/var/www/app" {
		t.Errorf("react: ExecWorkdir = %q, want /var/www/app", s.ExecWorkdir)
	}
}

// TestSvelteSpec locks the svelte SPA type: same pod shape as react —
// caddy serves the static bundle, app = sshd + node tooling.
func TestSvelteSpec(t *testing.T) {
	s, ok := SpecFor(TypeSvelte)
	if !ok {
		t.Fatal("no spec for svelte")
	}
	if s.Family != FamilyNode {
		t.Errorf("svelte: Family = %q, want node", s.Family)
	}
	if !s.HasAppRole {
		t.Error("svelte: HasAppRole = false, want true")
	}
	if s.ExecUser != "1000:1000" {
		t.Errorf("svelte: ExecUser = %q, want 1000:1000", s.ExecUser)
	}
	if s.ExecWorkdir != "/var/www/app" {
		t.Errorf("svelte: ExecWorkdir = %q, want /var/www/app", s.ExecWorkdir)
	}
	if s.DefaultRuntime.Node != "22" {
		t.Errorf("svelte: default node = %q, want 22", s.DefaultRuntime.Node)
	}
}

// TestSvelteKitSpec locks the server-side svelte type: nextjs shape —
// caddy proxies to the app server; no static bundle.
func TestSvelteKitSpec(t *testing.T) {
	s, ok := SpecFor(TypeSvelteKit)
	if !ok {
		t.Fatal("no spec for sveltekit")
	}
	if s.Family != FamilyNode {
		t.Errorf("sveltekit: Family = %q, want node", s.Family)
	}
	if !s.HasAppRole {
		t.Error("sveltekit: HasAppRole = false, want true")
	}
	if s.StaticBundle {
		t.Error("sveltekit: StaticBundle = true, want false (server type)")
	}
	if s.ExecUser != "1000:1000" {
		t.Errorf("sveltekit: ExecUser = %q, want 1000:1000", s.ExecUser)
	}
	if s.ExecWorkdir != "/var/www/app" {
		t.Errorf("sveltekit: ExecWorkdir = %q, want /var/www/app", s.ExecWorkdir)
	}
	if s.DefaultRuntime.Node != "22" {
		t.Errorf("sveltekit: default node = %q, want 22", s.DefaultRuntime.Node)
	}
	if s.DefaultRuntime.Database != DatabaseNone {
		t.Errorf("sveltekit: default db = %q, want none", s.DefaultRuntime.Database)
	}
}

// TestStaticBundle locks which types are SPAs (caddy serves the built
// bundle; TAINER_BUILD_DIR applies) vs server types (caddy proxies).
func TestStaticBundle(t *testing.T) {
	for typ, want := range map[ProjectType]bool{
		TypeReact:     true,
		TypeSvelte:    true,
		TypeSvelteKit: false,
		TypeNodeJS:    false,
		TypeNextJS:    false,
		TypeKompozi:   false,
	} {
		s, ok := SpecFor(typ)
		if !ok {
			t.Fatalf("no spec for %s", typ)
		}
		if s.StaticBundle != want {
			t.Errorf("%s: StaticBundle = %v, want %v", typ, s.StaticBundle, want)
		}
	}
}

// TestDefaultDatabases locks the per-type database default: PHP types use
// mariadb, kompozi needs postgres, every other node type starts without one.
func TestDefaultDatabases(t *testing.T) {
	want := map[ProjectType]DatabaseType{
		TypeWordPress: DatabaseMariaDB,
		TypePHP:       DatabaseMariaDB,
		TypeNodeJS:    DatabaseNone,
		TypeNextJS:    DatabaseNone,
		TypeNuxtJS:    DatabaseNone,
		TypeNestJS:    DatabaseNone,
		TypeReact:     DatabaseNone,
		TypeSvelte:    DatabaseNone,
		TypeSvelteKit: DatabaseNone,
		TypeKompozi:   DatabasePostgres,
	}
	for pt, db := range want {
		s, ok := SpecFor(pt)
		if !ok {
			t.Fatalf("no spec for %s", pt)
		}
		if s.DefaultRuntime.Database != db {
			t.Errorf("%s: default db = %q, want %q", pt, s.DefaultRuntime.Database, db)
		}
	}
}

// TestNodeFamilyDefaultsToNode22 pins every node-family type (kompozi
// included) to the node generation the stack actively tests.
func TestNodeFamilyDefaultsToNode22(t *testing.T) {
	n := 0
	for _, s := range typeSpecs {
		if s.Family != FamilyNode {
			continue
		}
		n++
		if s.DefaultRuntime.Node != "22" {
			t.Errorf("%s: default node = %q, want 22", s.Type, s.DefaultRuntime.Node)
		}
	}
	if n == 0 {
		t.Fatal("no node-family types found")
	}
}
