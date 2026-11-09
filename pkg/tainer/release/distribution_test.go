package release

import (
	"encoding/xml"
	"os"
	"testing"
)

// Both installers refuse to run below tainer's minimum macOS.
func TestDistributionsRequireMacOS14(t *testing.T) {
	for _, name := range []string{"distribution.xml", "distribution-menu.xml"} {
		b, err := os.ReadFile("../../../packaging/installer/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			OS []struct {
				Min string `xml:"min,attr"`
			} `xml:"allowed-os-versions>os-version"`
		}
		if err := xml.Unmarshal(b, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(doc.OS) != 1 || doc.OS[0].Min != "14.0" {
			t.Errorf("%s: allowed-os-versions = %+v, want one os-version min=\"14.0\"", name, doc.OS)
		}
	}
}
