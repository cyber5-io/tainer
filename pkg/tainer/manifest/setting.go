package manifest

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// SettableKeys are the keys `tainer config set` may write, in the order they
// are listed to users. All but "database" are dotted tainer.yaml paths.
var SettableKeys = []string{"pod.size", "database", "runtime.node", "runtime.php", "runtime.build-dir", "live.domain"}

// keyAliases maps accepted alternative spellings to their canonical key.
var keyAliases = map[string]string{"runtime.database": "database"}

// CanonicalSettingKey returns key's canonical spelling, or "" when key is
// neither a settable key nor an accepted alias.
func CanonicalSettingKey(key string) string {
	if c, ok := keyAliases[key]; ok {
		return c
	}
	for _, k := range SettableKeys {
		if k == key {
			return k
		}
	}
	return ""
}

// settingPath is where a canonical key lives in tainer.yaml.
func settingPath(key string) (section, field string) {
	if key == "database" {
		return "runtime", "database"
	}
	section, field, _ = strings.Cut(key, ".")
	return section, field
}

// UnknownSettingError is a key outside SettableKeys.
type UnknownSettingError struct{ Key string }

func (e *UnknownSettingError) Error() string {
	return fmt.Sprintf("unknown setting %q (expected one of %s)", e.Key, strings.Join(SettableKeys, ", "))
}

// InvalidSettingError is a value the manifest rules refuse for its key.
type InvalidSettingError struct{ Err error }

func (e *InvalidSettingError) Error() string { return e.Err.Error() }
func (e *InvalidSettingError) Unwrap() error { return e.Err }

// SettingValue reads a settable key (or alias) from m; an unset key reads as "".
func SettingValue(m *Manifest, key string) (string, error) {
	switch CanonicalSettingKey(key) {
	case "pod.size":
		if m.Pod == nil {
			return "", nil
		}
		return string(m.Pod.Size), nil
	case "database":
		return string(m.Runtime.Database), nil
	case "runtime.node":
		return m.Runtime.Node, nil
	case "runtime.php":
		return m.Runtime.PHP, nil
	case "runtime.build-dir":
		return m.Runtime.BuildDir, nil
	case "live.domain":
		if m.Live == nil {
			return "", nil
		}
		return m.Live.Domain, nil
	}
	return "", &UnknownSettingError{Key: key}
}

// ApplySetting sets key (canonical or alias) to value in raw, the bytes of a
// tainer.yaml, and returns the new file bytes. The value is checked by the
// full manifest validation first. The file is edited as a YAML node tree, so
// only the target key changes: comments, keys tainer does not know, key order
// and indentation survive. The local overlay is never involved, so a write
// never bakes local overrides into the shared file.
//
// stored is the value as written (normalised, e.g. a cleaned build dir).
// When it equals what the file holds, changed is false and out is raw itself.
// An empty live.domain removes the live section; leaving pod.size custom
// removes pod.memory and pod.cpu.
func ApplySetting(raw []byte, key, value string) (out []byte, stored string, changed bool, err error) {
	canon := CanonicalSettingKey(key)
	if canon == "" {
		return nil, "", false, &UnknownSettingError{Key: key}
	}
	current, err := ParseBytes(raw)
	if err != nil {
		return nil, "", false, err
	}
	before, _ := SettingValue(current, canon)
	next, _ := ParseBytes(raw)
	if err := setField(next, canon, value); err != nil {
		return nil, "", false, &InvalidSettingError{Err: err}
	}
	if err := next.validate(); err != nil {
		return nil, "", false, &InvalidSettingError{Err: err}
	}
	stored, _ = SettingValue(next, canon)
	if stored == before {
		return raw, stored, false, nil
	}

	out, err = editNode(raw, canon, stored)
	if err != nil {
		return nil, "", false, err
	}
	check, err := ParseBytes(out)
	if err != nil {
		return nil, "", false, fmt.Errorf("%s: the edited tainer.yaml does not load: %w", canon, err)
	}
	if got, _ := SettingValue(check, canon); got != stored {
		return nil, "", false, &InvalidSettingError{Err: fmt.Errorf("%s cannot be set in a version %d tainer.yaml", canon, current.Version)}
	}
	return out, stored, true, nil
}

func setField(m *Manifest, key, value string) error {
	kind := func() string { return fmt.Sprintf("%s is a %s project", m.Project.Name, m.Project.Type) }
	switch key {
	case "pod.size":
		if m.Pod == nil {
			m.Pod = &PodConfig{}
		}
		m.Pod.Size = PodSize(value)
		if m.Pod.Size != PodSizeCustom {
			m.Pod.Memory, m.Pod.CPU = "", 0
		}
	case "database":
		db := DatabaseType(value)
		// Choosing the value already set is no change, even on a hand-edited
		// file whose value the type doesn't offer.
		switch db {
		case m.Runtime.Database:
		case DatabaseMariaDB, DatabasePostgres, DatabaseNone:
			// A known engine the type doesn't offer; anything else gets
			// the manifest's own "invalid database" message.
			offered := DatabasesFor(m.Project.Type)
			if !containsDB(offered, db) {
				names := make([]string, len(offered))
				for i, d := range offered {
					names[i] = string(d)
				}
				return fmt.Errorf("database %q isn't offered for %s projects (choose %s)", value, m.Project.Type, orList(names))
			}
		}
		m.Runtime.Database = db
	case "runtime.node":
		if !m.IsNode() {
			return fmt.Errorf("runtime.node applies to node projects; %s", kind())
		}
		if value != "" && value != m.Runtime.Node && !slices.Contains(NodeVersions, value) {
			return fmt.Errorf("runtime.node %q isn't available (choose %s)", value, orList(NodeVersions))
		}
		m.Runtime.Node = value
	case "runtime.php":
		if !m.IsPHP() {
			return fmt.Errorf("runtime.php applies to php projects; %s", kind())
		}
		if value != "" && value != m.Runtime.PHP && !slices.Contains(PHPVersions, value) {
			return fmt.Errorf("runtime.php %q isn't available (choose %s)", value, orList(PHPVersions))
		}
		m.Runtime.PHP = value
	case "runtime.build-dir":
		if !m.IsSPA() {
			return fmt.Errorf("runtime.build-dir applies to react and svelte projects; %s", kind())
		}
		m.Runtime.BuildDir = value
	case "live.domain":
		if value == "" {
			m.Live = nil
		} else {
			m.Live = &LiveConfig{Domain: value}
		}
	default:
		return &UnknownSettingError{Key: key}
	}
	return nil
}

// editNode writes stored at key's path in the YAML document raw, changing
// nothing else.
func editNode(raw []byte, key, stored string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parsing manifest: tainer.yaml is not a mapping")
	}
	root := doc.Content[0]
	section, field := settingPath(key)
	switch {
	case key == "live.domain" && stored == "":
		removeKey(root, "live")
	case key == "runtime.build-dir" && stored == "":
		if m := mappingValue(root, section); m != nil {
			removeKey(m, field)
		}
	default:
		m, err := ensureMapping(root, section)
		if err != nil {
			return nil, err
		}
		setScalar(m, field, stored)
		if key == "pod.size" && stored != string(PodSizeCustom) {
			removeKey(m, "memory")
			removeKey(m, "cpu")
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(detectIndent(raw))
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("writing manifest: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("writing manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// detectIndent is the indent of the first indented mapping line; 2 when the
// file has none.
func detectIndent(raw []byte) int {
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		n := len(line) - len(trimmed)
		if n > 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "- ") {
			return n
		}
	}
	return 2
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// ensureMapping returns the mapping under key, creating it (or replacing an
// empty value such as `live:`) when needed.
func ensureMapping(m *yaml.Node, key string) (*yaml.Node, error) {
	if v := mappingValue(m, key); v != nil {
		switch {
		case v.Kind == yaml.MappingNode:
			return v, nil
		case v.Kind == yaml.ScalarNode && (v.Tag == "!!null" || v.Value == ""):
			*v = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			return v, nil
		default:
			return nil, fmt.Errorf("parsing manifest: %s is not a section", key)
		}
	}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
	return v, nil
}

// setScalar sets key's scalar value, keeping an existing node's quoting where
// it still reads back as the same string.
func setScalar(m *yaml.Node, key, value string) {
	tag, style := scalarTag(value)
	if v := mappingValue(m, key); v != nil && v.Kind == yaml.ScalarNode {
		v.Value, v.Tag = value, tag
		if v.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) == 0 {
			v.Style = style
		}
		return
	} else if v != nil {
		*v = yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value, Style: style}
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value, Style: style})
}

// scalarTag picks a plain style for values YAML reads back as the same text
// (strings, and numbers such as a node version) and double quotes otherwise.
func scalarTag(value string) (tag string, style yaml.Style) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(value), &n); err == nil && len(n.Content) == 1 && n.Content[0].Kind == yaml.ScalarNode && n.Content[0].Value == value {
		switch n.Content[0].Tag {
		case "!!str", "!!int", "!!float":
			return n.Content[0].Tag, 0
		}
	}
	return "!!str", yaml.DoubleQuotedStyle
}

func removeKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func containsDB(list []DatabaseType, d DatabaseType) bool { return slices.Contains(list, d) }

// orList joins values as "a, b or c".
func orList(values []string) string {
	if len(values) <= 1 {
		return strings.Join(values, "")
	}
	return strings.Join(values[:len(values)-1], ", ") + " or " + values[len(values)-1]
}
