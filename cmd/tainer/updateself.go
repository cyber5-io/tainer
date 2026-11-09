package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/imagecheck"
	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/registry"
	"github.com/cyber5-io/tainer/pkg/tainer/release"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// downloadPage is where tainer comes from where it can't update itself.
const downloadPage = "https://tainer.dev"

// tainerHintHere is printed by `tainer update` inside a project folder
// when tainer itself has an update.
const tainerHintHere = `A newer tainer is available — run "tainer update" outside a project folder to see everything.`

// Seams for tests.
var (
	releaseCheck                 = release.Check
	installedTainerFor           = installedTainerVersion
	installedMenuFor             = release.InstalledMenu
	podCheck                     = livePodCheck
	selfInstallTainer            = installTainer
	selfInstallMenu              = installMenu
	updatePod                    = liveUpdatePod
	updateStdin        io.Reader = os.Stdin
	runningPodNames              = liveRunningPodNames
	handOffUpdate                = liveHandOffUpdate
)

// checkPhaseTimeout bounds the checks before a listing, as --check does.
const checkPhaseTimeout = 2 * time.Minute

// liveRunningPodNames are the pods a tainer update would restart; none
// when tainer isn't running or they can't be listed.
func liveRunningPodNames(ctx context.Context) []string {
	entries, err := snapshotRunning(ctx, liveResumeOps())
	if err != nil {
		return nil
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

// liveHandOffUpdate runs `<installed tainer> update --yes` with this run's
// output flags, cwd and stdio, and returns its exit code: once tainer is
// updated, the rest is done by the new binary.
func liveHandOffUpdate(outArgs []string) int {
	cmd := exec.Command(installedTainerBin, append([]string{"update", "--yes"}, outArgs...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee) && ee.ExitCode() > 0:
		return ee.ExitCode()
	default:
		fmt.Fprintln(os.Stderr, "tainer: couldn't run the updated tainer to finish: ", err)
		return 1
	}
}

// tainerRelease is the `tainer` key of `update --check --json`.
type tainerRelease struct {
	Installed       string `json:"installed"`
	Available       string `json:"available"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Notes           string `json:"notes"`
}

// menuRelease is the `menu` key of `update --check --json`.
type menuRelease struct {
	Installed        string `json:"installed"`
	Available        string `json:"available"`
	UpdateAvailable  bool   `json:"updateAvailable"`
	Notes            string `json:"notes"`
	MinTainer        string `json:"minTainer"`
	NeedsTainerFirst bool   `json:"needsTainerFirst"`
}

// selfStatus is tainer and the menu app against latest.json.
type selfStatus struct {
	latest release.Latest
	ok     bool
	Tainer tainerRelease
	Menu   menuRelease
}

// menuWanted reports whether the menu app at installed has an update to
// available. A missing menu app is not an update: `tainer update menu`
// installs it, but listings don't offer it.
func menuWanted(available, installed string) bool {
	return available != "" && installed != "" && release.Newer(available, installed)
}

// checkSelf reads latest.json (cached unless force) and the installed
// versions. A failed fetch with no cache leaves ok false: no news.
func checkSelf(ctx context.Context, force bool) selfStatus {
	latest, ok := releaseCheck(ctx, force)
	st := selfStatus{latest: latest, ok: ok}
	st.Tainer.Installed = installedTainerFor()
	st.Menu.Installed = installedMenuFor()
	if !ok {
		return st
	}
	st.Tainer.Available = latest.Tainer.Version
	st.Tainer.Notes = latest.Tainer.Notes
	st.Tainer.UpdateAvailable = release.Newer(latest.Tainer.Version, st.Tainer.Installed)
	st.Menu.Available = latest.Menu.Version
	st.Menu.Notes = latest.Menu.Notes
	st.Menu.MinTainer = latest.Menu.MinTainer
	st.Menu.UpdateAvailable = menuWanted(latest.Menu.Version, st.Menu.Installed)
	st.Menu.NeedsTainerFirst = st.Menu.UpdateAvailable && latest.Menu.MinTainer != "" &&
		release.Newer(latest.Menu.MinTainer, st.Tainer.Installed)
	return st
}

// livePodCheck runs the image check over projects against the runtime.
func livePodCheck(ctx context.Context, autoStart bool, projects map[string]registry.Project) (imagecheck.Report, error) {
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: autoStart})
	if err != nil {
		return imagecheck.Report{}, engineErr(err)
	}
	defer eng.Close()
	cd := controlCheckDeps()
	deps := imagecheck.Deps{
		EnginePods:   func(ctx context.Context) ([]pod.Pod, error) { return pod.List(ctx, eng) },
		ImageInspect: func(ctx context.Context, ref string) (engine.ImageInfo, error) { return eng.ImageInspect(ctx, ref) },
		RemoteConfigDigest: func(ctx context.Context, ref imagecheck.Ref, arch string) (string, error) {
			return imagecheck.RemoteConfigDigest(ctx, nil, ref, arch)
		},
		RemoteLabels:  cd.RemoteLabels,
		TainerVersion: cd.TainerVersion,
	}
	rep, err := imagecheck.Run(ctx, deps, projects)
	if err != nil {
		return imagecheck.Report{}, engineErr(err)
	}
	return rep, nil
}

// liveUpdatePod updates one pod's images against the runtime.
func liveUpdatePod(ctx context.Context, name, manifestPath, dir string) (updateOutcome, error) {
	eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
	if err != nil {
		return updateOutcome{}, engineErr(err)
	}
	defer eng.Close()
	return updateProject(ctx, name, manifestPath, dir, liveControlUpdateOps(eng))
}

// signalContext is cancelled on Ctrl-C or SIGTERM, so an interrupted
// self-update leaves its resume file for whoever finishes the install.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// selfUpdateRun is one interactive `tainer update`: every prompt reads the
// same reader so buffered answers aren't lost.
type selfUpdateRun struct {
	in  *bufio.Reader
	out io.Writer
	yes bool
}

// installOpsFor wires an installer to this run's prompts, and to the
// latest.json the listing showed (so what is installed is what was listed).
func (r selfUpdateRun) installOpsFor(menu bool, st selfStatus) installOps {
	ops := liveInstallOps(r.yes)
	if menu {
		ops = liveMenuInstallOps(r.yes)
	}
	ops.in, ops.out = r.in, r.out
	if st.ok {
		latest := st.latest
		ops.check = func(context.Context) (release.Latest, bool) { return latest, true }
	}
	return ops
}

// tainerLine and menuLine describe a self-update in the listing.
func tainerLine(t tainerRelease) string {
	if !selfUpdateSupported {
		return fmt.Sprintf("tainer %s available — download it from %s", t.Available, downloadPage)
	}
	return fmt.Sprintf("tainer %s → %s", orNone(t.Installed), t.Available)
}

func menuLine(m menuRelease) string {
	if !selfUpdateSupported {
		return fmt.Sprintf("menu app %s available — download it from %s", m.Available, downloadPage)
	}
	s := fmt.Sprintf("menu app %s → %s", orNone(m.Installed), m.Available)
	if m.NeedsTainerFirst {
		s += fmt.Sprintf(" (needs tainer %s first)", m.MinTainer)
	}
	return s
}

func orNone(v string) string {
	if v == "" {
		return "not installed"
	}
	return v
}

// podLine describes a pod update in the listing; a running pod is
// restarted by it, so the line says so.
func podLine(p imagecheck.PodReport) string {
	s := p.Name
	if p.Parts != "" {
		s += "  " + p.Parts
	}
	if p.State == "running" {
		s += "  (restarts it)"
	}
	return s
}

// podsWithUpdates are the pods whose check found something newer.
func podsWithUpdates(rep imagecheck.Report) []imagecheck.PodReport {
	var out []imagecheck.PodReport
	for _, p := range rep.Pods {
		if p.UpdateAvailable {
			out = append(out, p)
		}
	}
	return out
}

// updateEverything is `tainer update` outside a project folder: it lists
// tainer, the menu app and every pod with an update, asks once, then
// updates tainer first, the menu app if still needed, then the pods.
// handOff is true when tainer itself was updated: the caller then lets the
// new binary do the rest.
func updateEverything(ctx context.Context, r selfUpdateRun) (handOff bool, err error) {
	checkCtx, cancel := context.WithTimeout(ctx, checkPhaseTimeout)
	st := checkSelf(checkCtx, false)
	all := registry.All()
	var pods, held []imagecheck.PodReport
	podsChecked := true
	if len(all) > 0 {
		rep, err := podCheck(checkCtx, false, all)
		if err != nil {
			podsChecked = false
		} else {
			pods, held = podsWithUpdates(rep), heldPods(rep)
		}
	}
	doTainer := st.Tainer.UpdateAvailable && selfUpdateSupported
	doMenu := st.Menu.UpdateAvailable && selfUpdateSupported
	var running []string
	if doTainer {
		running = runningPodNames(checkCtx)
	}
	cancel()

	var lines []string
	if st.Tainer.UpdateAvailable {
		lines = append(lines, tainerLine(st.Tainer))
	}
	if st.Menu.UpdateAvailable {
		lines = append(lines, menuLine(st.Menu))
	}
	for _, p := range pods {
		lines = append(lines, podLine(p))
	}
	for _, p := range held {
		lines = append(lines, p.Name+"  needs a newer tainer — "+heldDetail(p))
	}
	if !podsChecked {
		fmt.Fprintln(r.out, "pods weren't checked — tainer isn't running")
	}
	if !doTainer && !doMenu && len(pods) == 0 {
		for _, l := range lines {
			fmt.Fprintln(r.out, "  "+l)
		}
		if len(lines) == 0 && podsChecked {
			fmt.Fprintln(r.out, "everything is up to date")
		}
		return false, nil
	}
	fmt.Fprintln(r.out, "Updates available:")
	for _, l := range lines {
		fmt.Fprintln(r.out, "  "+l)
	}
	if len(running) > 0 {
		fmt.Fprintf(r.out, "Updating tainer restarts your running pods (%s).\n", strings.Join(running, ", "))
	}
	if !r.yes && !confirm(r.in, r.out, "Update all of these?") {
		fmt.Fprintln(r.out, "nothing updated")
		return false, nil
	}
	// That answer covers every step: the installers don't ask again.
	r.yes = true

	var errs []error
	if doTainer {
		res, err := selfInstallTainer(ctx, r.installOpsFor(false, st))
		if errors.Is(err, errUpdateInterrupted) || ctx.Err() != nil {
			return false, errors.Join(err, ctx.Err())
		}
		if err == nil && res.Updated {
			return true, nil
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	// The full pkg carries a menu app: if it was new enough, the separate
	// menu update is no longer needed.
	if doMenu {
		if now := installedMenuFor(); now == "" || release.Newer(st.Menu.Available, now) {
			if _, err := selfInstallMenu(ctx, r.installOpsFor(true, st)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, p := range pods {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		reg, ok := all[p.Name]
		if !ok {
			continue
		}
		if err := updateOnePod(ctx, r.out, p.Name, filepath.Join(reg.Path, manifest.FileName), reg.Path); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name, err))
		}
	}
	return false, errors.Join(errs...)
}

// updateHere is `tainer update` inside a project folder: only that pod is
// offered, plus a one-line hint when tainer itself has an update.
func updateHere(ctx context.Context, r selfUpdateRun, name, manifestPath, dir string) error {
	proj, ok := registry.Get(name)
	if !ok || proj.Path != dir {
		proj = registry.Project{Path: dir}
	}
	checkCtx, cancel := context.WithTimeout(ctx, checkPhaseTimeout)
	rep, err := podCheck(checkCtx, true, map[string]registry.Project{name: proj})
	if err != nil {
		cancel()
		return err
	}
	st := checkSelf(checkCtx, false)
	cancel()
	defer func() {
		if st.Tainer.UpdateAvailable {
			fmt.Fprintln(r.out, tainerHintHere)
		}
	}()

	var pr imagecheck.PodReport
	for _, p := range rep.Pods {
		if p.Name == name {
			pr = p
		}
	}
	switch {
	case pr.UpdateAvailable:
	case pr.Control != nil && pr.Control.State == imagecheck.ControlNeedsNewerTainer:
		fmt.Fprintf(r.out, "%s needs a newer tainer — %s\n", name, heldDetail(pr))
		return nil
	case pr.Error != "":
		fmt.Fprintf(r.out, "couldn't check %s — %s\n", name, pr.Error)
		return nil
	default:
		fmt.Fprintf(r.out, "%s is up to date\n", name)
		return nil
	}
	fmt.Fprintln(r.out, "Update available:")
	fmt.Fprintln(r.out, "  "+podLine(pr))
	if !r.yes && !confirm(r.in, r.out, "Update "+name+"?") {
		fmt.Fprintln(r.out, "nothing updated")
		return nil
	}
	return updateOnePod(ctx, r.out, name, manifestPath, dir)
}

// updateOnePod updates one pod and prints how it went.
func updateOnePod(ctx context.Context, out io.Writer, name, manifestPath, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	started := time.Now()
	fmt.Fprintf(out, "updating %s...\n", name)
	res, err := updatePod(ctx, name, manifestPath, dir)
	if err != nil {
		fmt.Fprintf(out, "couldn't update %s: %v\n", name, err)
		return err
	}
	if res.Note != "" {
		fmt.Fprintln(out, res.Note)
	}
	if !res.Background {
		fmt.Fprintf(out, "updated %s%s   (%.1fs)\n", name, stillStoppedNote(res.Started), time.Since(started).Seconds())
	}
	return nil
}

// runUpdateNoArgs is `tainer update` with no target.
// outArgs are the output flags to pass on when handing off to the new
// binary.
func runUpdateNoArgs(outMode tui.OutputMode, yes bool, outArgs []string) {
	r := selfUpdateRun{in: bufio.NewReader(updateStdin), out: os.Stdout, yes: yes}
	cwd, err := os.Getwd()
	must(err)
	manifestPath, dir, ferr := findManifestUpward(cwd)
	inProject := ferr == nil
	if outMode == tui.ModeJSON {
		// A JSON caller can't answer a question: inside a project it gets
		// the single-project document, as before; elsewhere it lists with
		// --check and updates each target.
		if inProject {
			runUpdateSingle(outMode, nil)
			return
		}
		must(withCode("invalid-arguments", errors.New("with --json, pass a target (tainer, menu or a project) or --check")))
		return
	}
	ctx, stop := signalContext()
	defer stop()
	if inProject {
		m, err := manifest.Load(manifestPath)
		must(withCode("manifest-invalid", err))
		must(updateHere(ctx, r, m.Project.Name, manifestPath, dir))
		return
	}
	handOff, err := updateEverything(ctx, r)
	must(err)
	if handOff {
		exitFn(handOffUpdate(outArgs))
	}
}

// outputFlagArgs turns the parsed output flags back into arguments.
func outputFlagArgs(f tui.OutputFlags) []string {
	var out []string
	if f.UI {
		out = append(out, "--ui")
	}
	if f.Plain {
		out = append(out, "--plain")
	}
	if f.JSON {
		out = append(out, "--json")
	}
	if f.NoColor {
		out = append(out, "--no-color")
	}
	return out
}

// reservedClash refuses a reserved target for a JSON caller when a project
// has the same name: a program must say which one it means.
func reservedClash(name string) error {
	if _, ok := registry.Get(name); !ok {
		return nil
	}
	return withCode("invalid-arguments", fmt.Errorf("%q is both a project and an update target; pass the project's folder path instead", name))
}

// reservedTargets always mean tainer itself, the menu app and the resume
// step; a project with one of these names is updated by its path.
var reservedTargets = map[string]bool{"tainer": true, "menu": true, "resume": true}

// isPathTarget reports whether arg names a project folder rather than a
// registered project.
func isPathTarget(arg string) bool {
	return strings.HasPrefix(arg, ".") || strings.ContainsRune(arg, filepath.Separator) || strings.Contains(arg, "/")
}

// locateUpdateTarget resolves `tainer update <arg>`: a path argument
// means the project in that folder, anything else a registered name.
func locateUpdateTarget(args []string) (manifestPath, projectDir string, err error) {
	if len(args) == 0 || !isPathTarget(args[0]) {
		return locateManifest(args)
	}
	dir, err := filepath.Abs(args[0])
	if err != nil {
		return "", "", err
	}
	manifestPath = filepath.Join(dir, manifest.FileName)
	if _, err := os.Stat(manifestPath); err != nil {
		return "", "", withCode("no-project-here", fmt.Errorf("no tainer project in %s", dir))
	}
	return manifestPath, dir, nil
}

// reservedHint is printed by a reserved target when a project has the
// same name.
func reservedHint(out io.Writer, name string) {
	if _, ok := registry.Get(name); ok {
		fmt.Fprintf(out, "(to update the project named %q, run tainer update ./%s)\n", name, name)
	}
}

// runUpdateSelf is `tainer update tainer` / `tainer update menu`.
func runUpdateSelf(outMode tui.OutputMode, target string, yes bool) {
	out := io.Writer(os.Stdout)
	if outMode == tui.ModeJSON {
		// Prompts and progress stay off the JSON document.
		out = os.Stderr
	}
	reservedHint(out, target)
	ctx, stop := signalContext()
	defer stop()

	menu := target == "menu"
	if !selfUpdateSupported {
		st := checkSelf(ctx, false)
		res := newInstallResult(target, st.Tainer.Installed, st.Tainer.Available)
		line := tainerLine(st.Tainer)
		avail := st.Tainer.UpdateAvailable
		if menu {
			res = newInstallResult(target, st.Menu.Installed, st.Menu.Available)
			line, avail = menuLine(st.Menu), st.Menu.UpdateAvailable
		}
		if !avail {
			line = "tainer is up to date"
			if menu {
				line = "the menu app is up to date"
			}
		}
		fmt.Fprintln(out, line)
		if outMode == tui.ModeJSON {
			jsonDoc(res)
		}
		return
	}

	r := selfUpdateRun{in: bufio.NewReader(updateStdin), out: out, yes: yes}
	ops := liveInstallOps(yes)
	install := selfInstallTainer
	if menu {
		ops = liveMenuInstallOps(yes)
		install = selfInstallMenu
	}
	ops.in, ops.out = r.in, r.out
	res, err := install(ctx, ops)
	if outMode == tui.ModeJSON {
		if err != nil {
			exitFn(jsonFail(errCodeOr(err, "update-failed"), err.Error()))
			return
		}
		jsonDoc(res)
		return
	}
	must(err)
}

// stripYes removes -y/--yes from anywhere in args, so they work after a
// target too (`tainer update menu -y`).
func stripYes(args []string) (rest []string, yes bool) {
	for _, a := range args {
		switch a {
		case "-y", "--yes", "-yes", "--y":
			yes = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, yes
}
