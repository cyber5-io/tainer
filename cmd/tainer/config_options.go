package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

const configOptionsUsage = "usage: tainer config options [project]"

// cmdConfigOptions implements `tainer config options [project]`: what each
// settable pod setting can be for this project's type, and what it is now.
// args are what follows "options", output flags already removed.
func cmdConfigOptions(args []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			must(withCode("invalid-arguments", fmt.Errorf("unknown flag %s (%s)", a, configOptionsUsage)))
		}
	}
	if len(args) > 1 {
		must(withCode("invalid-arguments", errors.New(configOptionsUsage)))
	}
	var project string
	if len(args) == 1 {
		project = args[0]
	}
	m, _ := loadManifestFor(project)
	o := manifest.OptionsFor(m)
	if jsonActive {
		emitConfigOptionsJSON(o)
		return
	}
	fmt.Print(configOptionsText(o))
}

func emitConfigOptionsJSON(o manifest.Options) { jsonDoc(o) }

func configOptionsText(o manifest.Options) string {
	sizes := make([]string, len(o.Size.Values))
	for i, s := range o.Size.Values {
		sizes[i] = fmt.Sprintf("%s %s/%s CPU", s.Name, s.Memory, strconv.FormatFloat(s.CPU, 'f', -1, 64))
	}
	dbs := make([]string, len(o.Database.Values))
	for i, d := range o.Database.Values {
		dbs[i] = string(d)
	}
	var b strings.Builder
	line := func(name, current, choices string) {
		if choices != "" {
			choices = "(" + choices + ")"
		}
		fmt.Fprintf(&b, "%s", strings.TrimRight(fmt.Sprintf("%-10s %-8s %s", name, current, choices), " ")+"\n")
	}
	line("size", string(o.Size.Current), strings.Join(sizes, ", "))
	line("database", string(o.Database.Current), strings.Join(dbs, ", "))
	line(o.Runtime.Name, o.Runtime.Current, strings.Join(o.Runtime.Values, ", "))
	if o.BuildDir.Applies {
		line("build-dir", o.BuildDir.Current, "")
	}
	return b.String()
}
