package wizard

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

// TestDefaultDatabaseIsSelectable ensures the preselected database for each
// type matches the TypeSpec and appears in that type's choice list, and that
// postgres and mariadb stay selectable everywhere.
func TestDefaultDatabaseIsSelectable(t *testing.T) {
	for _, s := range manifest.AllSpecs() {
		def := string(defaultDatabase(s.Type))
		if def != string(s.DefaultRuntime.Database) {
			t.Errorf("%s: wizard default %q != spec %q", s.Type, def, s.DefaultRuntime.Database)
		}
		choices := dbChoices(s.Type)
		has := func(c string) bool {
			for _, x := range choices {
				if x == c {
					return true
				}
			}
			return false
		}
		if !has(def) {
			t.Errorf("%s: default %q not in choices %v", s.Type, def, choices)
		}
		if s.Type != manifest.TypeWordPress && !(has("postgres") && has("mariadb")) {
			t.Errorf("%s: postgres and mariadb must be selectable, got %v", s.Type, choices)
		}
	}
}
