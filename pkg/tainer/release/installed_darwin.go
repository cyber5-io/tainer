package release

import (
	"encoding/xml"
	"os"
	"strings"
)

// menuPlistPath is a var so tests can point at a fixture.
var menuPlistPath = "/Applications/Tainer Menu.app/Contents/Info.plist"

// InstalledMenu returns the installed menu app's version, or "".
func InstalledMenu() string {
	f, err := os.Open(menuPlistPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
	dec.Strict = false
	var lastKey string
	var inKey, inString bool
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			inKey, inString = t.Name.Local == "key", t.Name.Local == "string"
		case xml.EndElement:
			inKey, inString = false, false
		case xml.CharData:
			if inKey {
				lastKey = strings.TrimSpace(string(t))
			} else if inString && lastKey == "CFBundleShortVersionString" {
				return strings.TrimSpace(string(t))
			}
		}
	}
}
