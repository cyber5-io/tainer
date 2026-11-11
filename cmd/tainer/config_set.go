package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

const (
	configSetUsage          = "usage: tainer config set [project] <key> <value>"
	configSetNoteNextStart  = "takes effect on the next start"
	configSetNoteAlreadySet = "already set"
)

type configSetResult struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Changed bool   `json:"changed"`
	Note    string `json:"note"`
}

// cmdConfigSet implements `tainer config set [project] <key> <value>`: write
// one of manifest.SettableKeys to the project's tainer.yaml. args are what
// follows "set", output flags already removed. Arity alone picks the form:
// two arguments resolve the project from the working directory, three name
// it first (looked up in the registry, like `tainer config <project>`).
func cmdConfigSet(args []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			must(withCode("invalid-arguments", fmt.Errorf("unknown flag %s (%s)", a, configSetUsage)))
		}
	}
	if len(args) < 2 || len(args) > 3 {
		must(withCode("invalid-arguments", errors.New(configSetUsage)))
	}
	if len(args) == 2 && manifest.CanonicalSettingKey(args[0]) == "" && manifest.CanonicalSettingKey(args[1]) != "" {
		must(withCode("invalid-arguments", fmt.Errorf("missing the value for %s (to change it for project %s: tainer config set %s %s <value>)", args[1], args[0], args[0], args[1])))
	}
	var project string
	if len(args) == 3 {
		project, args = args[0], args[1:]
	}
	res, err := configSet(args[0], args[1], project)
	must(err)
	if jsonActive {
		emitConfigSetJSON(res)
		return
	}
	fmt.Println(res.line())
}

// configSet applies the setting to tainer.yaml (never the local overlay) and
// writes the file only when the stored value changes. The result names the
// key by its canonical spelling.
func configSet(key, value, project string) (configSetResult, error) {
	var locator []string
	if project != "" {
		locator = []string{project}
	}
	path, _, err := locateManifest(locator)
	if err != nil {
		return configSetResult{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return configSetResult{}, withCode("project-not-found", err)
		}
		return configSetResult{}, withCode("manifest-invalid", err)
	}
	out, stored, changed, err := manifest.ApplySetting(raw, key, value)
	if err != nil {
		var unknown *manifest.UnknownSettingError
		var invalid *manifest.InvalidSettingError
		if errors.As(err, &unknown) || errors.As(err, &invalid) {
			return configSetResult{}, withCode("invalid-arguments", err)
		}
		return configSetResult{}, withCode("manifest-invalid", err)
	}
	key = manifest.CanonicalSettingKey(key)
	res := configSetResult{Key: key, Value: stored, Changed: changed, Note: configSetNoteAlreadySet}
	if !changed {
		return res, nil
	}
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(path, out, mode); err != nil {
		return configSetResult{}, withCode("command-failed", err)
	}
	eff, err := manifest.Load(path)
	switch {
	case err != nil:
		res.Note = fmt.Sprintf("saved, but the project no longer loads: %v", err)
	default:
		res.Note = configSetNoteNextStart
		if got, _ := manifest.SettingValue(eff, key); got != stored {
			res.Note = fmt.Sprintf("saved, but %s sets %s to %s, which wins", manifest.LocalOverlayName, key, got)
		}
	}
	return res, nil
}

func (r configSetResult) line() string {
	switch {
	case !r.Changed:
		return fmt.Sprintf("%s is already %s", r.Key, r.Value)
	case r.Value == "":
		return fmt.Sprintf("%s cleared — %s", r.Key, r.Note)
	default:
		return fmt.Sprintf("%s set to %s — %s", r.Key, r.Value, r.Note)
	}
}

func emitConfigSetJSON(r configSetResult) { jsonDoc(r) }
