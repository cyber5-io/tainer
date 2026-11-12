#!/bin/bash
#
# Tests for legacy-cleanup.sh. Each case builds a podman-era tainer install
# under a scratch root and runs the script there with fake launchctl, pgrep,
# pkill, pkgutil, plutil, sudo and brew that only record what they were asked.
#
#   bash scripts/legacy-cleanup_test.sh
#
# Runs on the bash that ships with macOS (3.2) and on Linux.
set -u

HERE=$(cd "$(dirname "$0")" && pwd)
SCRIPT="$HERE/legacy-cleanup.sh"
FAILS=0
PASSES=0

fail() {
	FAILS=$((FAILS + 1))
	printf '    FAIL: %s\n' "$*"
}
pass() { PASSES=$((PASSES + 1)); }

check() { # check DESCRIPTION COMMAND...
	local what="$1"
	shift
	if "$@"; then pass; else fail "$what"; fi
}
present() { [ -e "$ROOT$1" ] || [ -L "$ROOT$1" ]; }
absent() { ! present "$1"; }
logged() { grep -qF -- "$1" "$LOG"; }
not_logged() { ! grep -qF -- "$1" "$LOG"; }
said() { printf '%s' "$OUT" | grep -qF -- "$1"; }
not_said() { ! said "$1"; }
booted_out() { grep -q "^launchctl bootout.*$1" "$LOG"; }
not_booted_out() { ! booted_out "$1"; }

file() { # file PATH [CONTENT]
	mkdir -p "$(dirname "$ROOT$1")"
	printf '%s' "${2:-x}" >"$ROOT$1"
}

helper_plist() { # helper_plist PATH LABEL PROGRAM
	file "$1" "<?xml version=\"1.0\" encoding=\"UTF-8\"?>
<plist version=\"1.0\">
<dict>
	<key>Label</key>
	<string>$2</string>
	<key>ProgramArguments</key>
	<array>
		<string>$3</string>
		<string>service</string>
	</array>
</dict>
</plist>
"
}

# Fake system commands. Each appends its arguments to $LOG.
make_fakes() {
	FAKE="$WORK/bin"
	mkdir -p "$FAKE"
	cat >"$FAKE/launchctl" <<EOF
#!/bin/bash
echo "launchctl \$*" >> "$LOG"
EOF
	# ps lists $WORK/ps; kill logs and drops the pids it was given from that list.
	cat >"$FAKE/ps" <<EOF
#!/bin/bash
cat "$WORK/ps"
EOF
	cat >"$FAKE/kill" <<EOF
#!/bin/bash
echo "kill \$*" >> "$LOG"
for a in "\$@"; do
	case "\$a" in -*) continue ;; esac
	grep -v "^ *\$a " "$WORK/ps" > "$WORK/ps.new"; mv "$WORK/ps.new" "$WORK/ps"
done
EOF
	cat >"$FAKE/pkgutil" <<EOF
#!/bin/bash
echo "pkgutil \$*" >> "$LOG"
case "\$1 \$2" in
"--pkg-info io.cyber5.tainer") [ -s "$WORK/receipt-tainer" ] && echo "version: \$(cat "$WORK/receipt-tainer")" && exit 0; exit 1 ;;
"--pkg-info com.redhat.podman") [ -s "$WORK/receipt-podman" ] && echo "version: 5.0.0" && exit 0; exit 1 ;;
"--forget io.cyber5.tainer") : > "$WORK/receipt-tainer" ;;
"--forget com.redhat.podman") : > "$WORK/receipt-podman" ;;
esac
EOF
	# plutil on the JSON connections file: -extract reads a key, -remove deletes one.
	cat >"$FAKE/plutil" <<EOF
#!/bin/bash
echo "plutil \$*" >> "$LOG"
case "\$1 \$2" in
"-extract Connection.Default") sed -n 's/.*"Default":"\([^"]*\)".*/\1/p' "\$4" | grep . ;;
"-extract "*) f="\${@: -1}"; grep -q "\"\${2##*.}\":" "\$f" ;;
"-remove Connection.Default") sed 's/"Default":"[^"]*",\{0,1\}//' "\$3" > "\$3.new" && mv "\$3.new" "\$3" ;;
"-remove "*) grep -q "\"\${2##*.}\":" "\$3" || exit 1
	sed "s/\"\${2##*.}\":{},\{0,1\}//" "\$3" > "\$3.new" && mv "\$3.new" "\$3" ;;
esac
EOF
	cat >"$FAKE/sudo" <<EOF
#!/bin/bash
echo "sudo \$*" >> "$LOG"
[ "\$1" = -u ] && shift 2
[ "\$1" = -H ] && shift
exec "\$@"
EOF
	cat >"$FAKE/brew" <<EOF
#!/bin/bash
echo "brew \$*" >> "$LOG"
case "\$1" in
list) grep -qx "\$3" "$WORK/formulas" ;;
uninstall) grep -vx "\$3" "$WORK/formulas" > "$WORK/formulas.new"; mv "$WORK/formulas.new" "$WORK/formulas" ;;
esac
EOF
	chmod +x "$FAKE"/*
}

setup() {
	WORK=$(mktemp -d "${TMPDIR:-/tmp}/legacy-cleanup-test.XXXXXX")
	ROOT="$WORK/root"
	LOG="$WORK/log"
	mkdir -p "$ROOT"
	: >"$LOG"
	: >"$WORK/ps"
	: >"$WORK/formulas"
	: >"$WORK/receipt-tainer"
	: >"$WORK/receipt-podman"
	make_fakes
	USERS_SPEC="alice:7001 bob:7002"
}

# The full podman-era picture, with the current tainer installed alongside.
populate() {
	# 0.1.8+ helper (tainer name) and the pre-0.1.8 one (podman's name, tainer-mac-helper).
	helper_plist /Library/LaunchDaemons/com.github.containers.tainer.helper-alice.plist \
		com.github.containers.tainer.helper-alice /usr/local/tainer/helper/alice/tainer-mac-helper
	helper_plist /Library/LaunchDaemons/com.github.containers.podman.helper-alice.plist \
		com.github.containers.podman.helper-alice /usr/local/podman/helper/alice/tainer-mac-helper
	file /usr/local/tainer/helper/alice/tainer-mac-helper
	file /usr/local/podman/helper/alice/tainer-mac-helper
	file /private/var/run/tainer-helper-alice.socket
	file /private/var/run/podman-helper-alice.socket
	# bob runs upstream podman's own helper: not ours.
	helper_plist /Library/LaunchDaemons/com.github.containers.podman.helper-bob.plist \
		com.github.containers.podman.helper-bob /usr/local/podman/helper/bob/podman-mac-helper
	file /usr/local/podman/helper/bob/podman-mac-helper
	file /private/var/run/podman-helper-bob.socket

	file /etc/paths.d/00-tainer /opt/tainer/bin
	file /usr/local/etc/man.d/tainer.man.conf
	file /etc/pf.anchors/tainer
	file /etc/pf.conf.tainer-backup
	file /etc/pf.conf 'scrub-anchor "com.apple/*"
rdr-anchor "tainer"
dummynet-anchor "com.apple/*"
anchor "com.apple/*"
load anchor "com.apple" from "/etc/pf.anchors/com.apple"
load anchor "tainer" from "/etc/pf.anchors/tainer"
'
	mkdir -p "$ROOT/opt/podman" "$ROOT/var/run"
	ln -s /opt/tainer/lib "$ROOT/opt/podman/lib"
	ln -s /Users/alice/.local/share/containers/tainer/machine/tainer.sock "$ROOT/var/run/docker.sock"

	file /Users/alice/.local/share/containers/tainer/machine/applehv/tainer-machine-default-arm64.raw
	file /Users/alice/.local/share/containers/tainer/machine/tainer.sock
	file /Users/alice/.config/containers/tainer/machine/applehv/tainer-machine-default.json
	file /Users/alice/.local/share/containers/podman/machine/libkrun/tainer-machine-default-arm64.raw
	file /Users/alice/.config/containers/podman/machine/libkrun/tainer-machine-default.json
	file /Users/alice/.local/share/containers/podman/machine/machine.pub
	file /Users/alice/.config/containers/podman-connections.json \
		'{"Connection":{"Default":"tainer-machine-default-root","Connections":{"tainer-machine-default":{},"tainer-machine-default-root":{},"podman-machine-default":{}}},"Farm":{}}'
	file /private/var/folders/alice/T/tainer/tainer-machine-default-api.sock

	file /Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist
	echo socket_vmnet >"$WORK/formulas"
	cat >"$WORK/ps" <<'PS'
 4242 /opt/tainer/bin/vfkit --device virtio-blk,path=/Users/alice/.local/share/containers/tainer/machine/applehv/tainer-machine-default-arm64.raw
 4243 /usr/local/tainer/helper/alice/tainer-mac-helper service /Users/alice/.local/share/containers/tainer/machine/tainer.sock
 5001 /usr/bin/vim /Users/alice/.local/share/containers/tainer/machine/notes.txt
 5002 /opt/tainer/bin/vfkit --device virtio-blk,path=/Users/alice/.cyberstack/disk.img
 5003 /usr/bin/tail -f /tmp/tainer-machine-default.log
PS
}

# What the current tainer (and other software) owns: must survive every run.
GUARDED="/opt/tainer/bin/tainer
/opt/tainer/bin/cyberstackd
/opt/tainer/bin/vfkit
/opt/tainer/share/tainer/certs/tainer.me.key
/Users/alice/.config/tainer/projects.json
/Users/alice/.cyberstack/cyberstack.sock
/Applications/Tainer Menu.app/Contents/Info.plist
/etc/resolver/tainer.me
/usr/local/bin/tainer
/Library/LaunchAgents/io.cyber5.tainer.menu.plist
/Library/LaunchDaemons/io.cyber5.cyberstack.pf.plist
/Library/LaunchDaemons/com.example.other.plist"

populate_guarded() {
	local f
	while IFS= read -r f; do file "$f"; done <<EOF
$GUARDED
EOF
	echo 1.5.8 >"$WORK/receipt-tainer"
}

run() {
	OUT=$(LEGACY_CLEANUP_ROOT="$ROOT" LEGACY_USERS="$USERS_SPEC" \
		LAUNCHCTL="$FAKE/launchctl" PS="$FAKE/ps" KILL="$FAKE/kill" \
		PKGUTIL="$FAKE/pkgutil" PLUTIL="$FAKE/plutil" SUDO="$FAKE/sudo" \
		BREW="$FAKE/brew" BREW_USER="#7001" \
		bash "$SCRIPT" "$@" 2>&1)
	STATUS=$?
}

snapshot() {
	(cd "$ROOT" && find . -print | sort | while IFS= read -r f; do
		if [ -L "$f" ]; then echo "$f -> $(readlink "$f")"; elif [ -f "$f" ]; then echo "$f $(cksum <"$f")"; else echo "$f"; fi
	done)
}

guarded_intact() {
	local f ok=0
	while IFS= read -r f; do
		present "$f" || {
			echo "      guarded path removed: $f"
			ok=1
		}
	done <<EOF
$GUARDED
EOF
	return $ok
}

teardown() { rm -rf "$WORK"; }

# ---------------------------------------------------------------------------

test_dry_run_changes_nothing() {
	setup
	populate
	populate_guarded
	local before after
	before=$(snapshot)
	run --dry-run
	after=$(snapshot)
	check "dry run exits 0" [ "$STATUS" -eq 0 ]
	check "dry run leaves every file as it was" [ "$before" = "$after" ]
	check "dry run boots nothing out" not_logged "launchctl bootout"
	check "dry run kills nothing" not_logged "kill "
	check "dry run edits no connections" not_logged "plutil -remove"
	check "dry run forgets no receipt" not_logged "--forget"
	check "dry run uninstalls nothing" not_logged "brew uninstall"
	check "dry run names the helper plist" said "would remove: $ROOT/Library/LaunchDaemons/com.github.containers.tainer.helper-alice.plist"
	check "dry run names the old VM" said "would remove: $ROOT/Users/alice/.local/share/containers/tainer"
	check "dry run lists the processes it would stop" said "would stop these old tainer processes:"
	check "dry run shows the VM's command line" said "4242 /opt/tainer/bin/vfkit --device"
	check "dry run shows the helper's command line" said "4243 /usr/local/tainer/helper/alice/tainer-mac-helper"
	check "dry run leaves an editor out" not_said "5001"
	check "dry run leaves the current tainer's VM out" not_said "5002"
	check "dry run leaves a bare name match out" not_said "5003"
	check "dry run says how to go ahead" said "Run again without --dry-run"
	check "dry run lists each path once" [ -z "$(printf '%s\n' "$OUT" | grep '^would remove:' | sort | uniq -d)" ]
	check "dry run count matches its list" said "Found $(printf '%s\n' "$OUT" | grep -c '^would \(remove\|stop these\)') item(s)"
	teardown
}

test_removes_the_old_tainer() {
	setup
	populate
	populate_guarded
	run
	check "exits 0" [ "$STATUS" -eq 0 ]
	local f
	for f in /Library/LaunchDaemons/com.github.containers.tainer.helper-alice.plist \
		/Library/LaunchDaemons/com.github.containers.podman.helper-alice.plist \
		/usr/local/tainer /usr/local/podman/helper/alice \
		/private/var/run/tainer-helper-alice.socket /private/var/run/podman-helper-alice.socket \
		/etc/paths.d/00-tainer /usr/local/etc/man.d/tainer.man.conf \
		/etc/pf.anchors/tainer /etc/pf.conf.tainer-backup /opt/podman /var/run/docker.sock \
		/Users/alice/.local/share/containers/tainer /Users/alice/.config/containers/tainer \
		/Users/alice/.local/share/containers/podman/machine/libkrun/tainer-machine-default-arm64.raw \
		/Users/alice/.config/containers/podman/machine/libkrun/tainer-machine-default.json \
		/private/var/folders/alice/T/tainer \
		/Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist; do
		check "removes $f" absent "$f"
	done
	check "keeps the current tainer and other software" guarded_intact
	check "keeps bob's podman helper" present /Library/LaunchDaemons/com.github.containers.podman.helper-bob.plist
	check "keeps bob's podman helper binary" present /usr/local/podman/helper/bob/podman-mac-helper
	check "keeps podman's own machine data" present /Users/alice/.local/share/containers/podman/machine/machine.pub
	check "says podman's data was kept" said "pass --remove-podman-data"
	check "pf.conf loses only the old tainer's lines" [ "$(grep -c tainer "$ROOT/etc/pf.conf")" -eq 0 ]
	check "pf.conf keeps Apple's anchors" [ "$(grep -c com.apple "$ROOT/etc/pf.conf")" -eq 4 ]
	check "boots out the 0.1.8+ helper" logged "launchctl bootout system/com.github.containers.tainer.helper-alice"
	check "boots out the pre-0.1.8 helper" logged "launchctl bootout system/com.github.containers.podman.helper-alice"
	check "leaves bob's podman helper running" not_logged "helper-bob"
	check "boots out socket_vmnet" logged "launchctl bootout system/homebrew.mxcl.socket_vmnet"
	check "stops the old VM and helper with SIGTERM" logged "kill -TERM 4242 4243"
	check "leaves the editor, the current VM and tail running" [ "$(grep -c '^ *500[123] ' "$WORK/ps")" -eq 3 ]
	check "never sends SIGKILL" not_logged "-9"
	check "never sends SIGKILL by name" not_logged "-KILL"
	check "drops the old tainer's connection" logged "plutil -remove Connection.Connections.tainer-machine-default "
	check "drops the old tainer's root connection" logged "plutil -remove Connection.Connections.tainer-machine-default-root"
	check "clears a default that pointed at the old tainer" logged "plutil -remove Connection.Default"
	check "uninstalls socket_vmnet as the Homebrew owner" logged "sudo -u #7001 -H $FAKE/brew uninstall --formula socket_vmnet"
	check "leaves the current tainer's receipt" not_logged "--forget io.cyber5.tainer"
	check "never boots out io.cyber5 items" not_booted_out "io\\.cyber5"
	check "names what it removed" said "removed: $ROOT/Library/LaunchDaemons/com.github.containers.tainer.helper-alice.plist"
	check "summarises" said "Done: removed"
	teardown
}

test_second_run_finds_nothing() {
	setup
	populate
	populate_guarded
	run
	: >"$LOG"
	local before
	before=$(snapshot)
	run
	check "second run exits 0" [ "$STATUS" -eq 0 ]
	check "second run finds nothing" said "Nothing from the old tainer was found"
	check "second run removes nothing" [ "$(printf '%s' "$OUT" | grep -c '^removed:')" -eq 0 ]
	check "second run changes no file" [ "$before" = "$(snapshot)" ]
	check "second run boots nothing out" not_logged "launchctl bootout"
	check "second run kills nothing" not_logged "kill -TERM"
	teardown
}

test_clean_mac_finds_nothing() {
	setup
	populate_guarded
	local before
	before=$(snapshot)
	run
	check "clean Mac: exits 0" [ "$STATUS" -eq 0 ]
	check "clean Mac: nothing found" said "Nothing from the old tainer was found"
	check "clean Mac: nothing kept either" not_said "Kept"
	check "clean Mac: no file changed" [ "$before" = "$(snapshot)" ]
	teardown
}

test_helper_plists_matched_per_user() {
	setup
	USERS_SPEC="alice:7001 carol:7003"
	helper_plist /Library/LaunchDaemons/com.github.containers.tainer.helper-alice.plist \
		com.github.containers.tainer.helper-alice /usr/local/tainer/helper/alice/tainer-mac-helper
	helper_plist /Library/LaunchDaemons/com.github.containers.tainer.helper-carol.plist \
		com.github.containers.tainer.helper-carol /usr/local/tainer/helper/carol/tainer-mac-helper
	# A user since deleted, and a plist with no Label: the file name stands in.
	helper_plist /Library/LaunchDaemons/com.github.containers.tainer.helper-dave.plist \
		com.github.containers.tainer.helper-dave /usr/local/tainer/helper/dave/tainer-mac-helper
	file /Library/LaunchDaemons/com.github.containers.tainer.helper-erin.plist '<plist><dict></dict></plist>'
	file /usr/local/tainer/helper/alice/tainer-mac-helper
	file /usr/local/tainer/helper/carol/tainer-mac-helper
	# Upstream podman's helper whose binary is gone: dead, removed.
	helper_plist /Library/LaunchDaemons/com.github.containers.podman.helper-frank.plist \
		com.github.containers.podman.helper-frank /usr/local/podman/helper/frank/podman-mac-helper
	run
	local u
	for u in alice carol dave erin; do
		check "removes $u's helper plist" absent "/Library/LaunchDaemons/com.github.containers.tainer.helper-$u.plist"
		check "boots out $u's helper" logged "launchctl bootout system/com.github.containers.tainer.helper-$u"
	done
	check "removes the helper binaries" absent /usr/local/tainer
	check "removes a podman helper whose binary is gone" absent /Library/LaunchDaemons/com.github.containers.podman.helper-frank.plist
	teardown
}

test_remove_podman_data_flag() {
	setup
	populate
	echo 5.0.0 >"$WORK/receipt-podman"
	file /etc/paths.d/podman-pkg /opt/podman/bin
	file /usr/local/etc/man.d/podman.man.conf
	run
	check "without the flag: podman's installer leftovers stay" present /etc/paths.d/podman-pkg
	check "without the flag: podman receipt stays" not_logged "--forget com.redhat.podman"
	run --remove-podman-data
	check "flag: removes bob's podman helper" absent /Library/LaunchDaemons/com.github.containers.podman.helper-bob.plist
	check "flag: removes podman's machine data" absent /Users/alice/.local/share/containers/podman
	check "flag: removes podman's machine config" absent /Users/alice/.config/containers/podman
	check "flag: removes podman's installer leftovers" absent /etc/paths.d/podman-pkg
	check "flag: forgets podman's receipt" logged "--forget com.redhat.podman"
	check "flag: keeps podman's connections file" present /Users/alice/.config/containers/podman-connections.json
	teardown
}

test_podman_installed_keeps_its_leftovers() {
	setup
	file /opt/podman/bin/podman
	chmod +x "$ROOT/opt/podman/bin/podman"
	file /etc/paths.d/podman-pkg /opt/podman/bin
	run --remove-podman-data
	check "an installed podman keeps its PATH entry" present /etc/paths.d/podman-pkg
	check "an installed podman keeps its binary" present /opt/podman/bin/podman
	teardown
}

test_socket_vmnet_kept_for_lima() {
	setup
	file /Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist
	echo socket_vmnet >"$WORK/formulas"
	mkdir -p "$ROOT/Users/bob/.lima"
	run
	check "Lima present: socket_vmnet stays" present /Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist
	check "Lima present: not uninstalled" not_logged "brew uninstall"
	check "Lima present: says why" said "Lima or Colima uses it too"
	run --remove-socket-vmnet
	check "flag: socket_vmnet removed" absent /Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist
	check "flag: uninstalled" logged "brew uninstall --formula socket_vmnet"
	teardown
}

test_old_receipt_forgotten() {
	setup
	echo 0.2.0 >"$WORK/receipt-tainer"
	run
	check "forgets a 0.x tainer receipt" logged "--forget io.cyber5.tainer"
	teardown
}

fork_opt_tainer() {
	file /opt/tainer/bin/tainer
	file /opt/tainer/bin/tainer-mac-helper
	file /opt/tainer/bin/gvproxy
	file /opt/tainer/bin/vfkit
	file /opt/tainer/bin/krunkit
	file /opt/tainer/lib/libkrun-efi.dylib
	file /opt/tainer/certs/tainer.me.key
	file /opt/tainer/docs/man/man1/tainer.1
	file /opt/tainer/bin/uninstall.sh
}

test_fork_opt_tainer_removed() {
	setup
	fork_opt_tainer
	run --dry-run
	check "fork /opt/tainer: dry run lists it" said "would remove: $ROOT/opt/tainer (the old tainer's programs)"
	check "fork /opt/tainer: dry run keeps it" present /opt/tainer/bin/tainer-mac-helper
	run
	check "fork /opt/tainer: removed" absent /opt/tainer
	run
	check "fork /opt/tainer: second run finds nothing" said "Nothing from the old tainer was found"
	teardown
}

test_one_fork_marker_is_enough() {
	local m
	for m in bin/tainer-mac-helper bin/krunkit lib/libkrun-efi.dylib certs/tainer.me.crt docs/man/man1/tainer.1 bin/uninstall.sh; do
		setup
		file /opt/tainer/bin/tainer
		file "/opt/tainer/$m"
		run
		check "fork marker $m: /opt/tainer removed" absent /opt/tainer
		teardown
	done
}

test_opt_tainer_kept_unless_positively_the_fork() {
	local layout
	for layout in current cli-only broken-current unrecognised mixed; do
		setup
		case "$layout" in
		current)
			file /opt/tainer/bin/tainer
			file /opt/tainer/bin/cyberstackd
			file /opt/tainer/bin/vfkit
			file /opt/tainer/share/cyberstack/cyberstack-boot-arm64.img
			;;
		cli-only) file /opt/tainer/bin/tainer ;; # the CLI-only package: no cyberstackd, no fork markers
		broken-current) # cyberstackd lost, rest of a current install intact
			file /opt/tainer/bin/tainer
			file /opt/tainer/bin/tainer-uninstall.sh
			file /opt/tainer/share/tainer/certs/tainer.me.key
			;;
		unrecognised) file /opt/tainer/bin/something-else ;;
		mixed) # a fork marker next to a current-only file: not positively the fork
			file /opt/tainer/bin/krunkit
			file /opt/tainer/bin/cyberstack-pf
			;;
		esac
		local before
		before=$(snapshot)
		run
		check "$layout /opt/tainer: kept" present /opt/tainer/bin
		check "$layout /opt/tainer: untouched" [ "$before" = "$(snapshot)" ]
		check "$layout /opt/tainer: not listed" not_said "opt/tainer (the old tainer"
		teardown
	done
}

test_never_follows_links_out_of_a_home() {
	setup
	file /opt/tainer/bin/cyberstackd
	file /opt/tainer/bin/tainer
	file /opt/podman/bin/keep-me
	mkdir -p "$ROOT/Users/alice/.local/share" "$ROOT/Users/alice/.config"
	ln -s "$ROOT/opt" "$ROOT/Users/alice/.local/share/containers"
	ln -s "$ROOT/opt" "$ROOT/Users/alice/.config/containers"
	run --remove-podman-data
	check "link trap: the current /opt/tainer survives" present /opt/tainer/bin/cyberstackd
	check "link trap: /opt/podman survives" present /opt/podman/bin/keep-me
	check "link trap: says it left it alone" said "leads outside $ROOT/Users/alice, so it was left alone"
	check "link trap: no rm ran on it" not_logged "rm -rf -- $ROOT/Users/alice/.local/share/containers/tainer"
	teardown
}

test_home_files_removed_as_their_owner() {
	setup
	populate
	run
	check "the old VM is removed as alice" logged "sudo -u #7001 -H rm -rf -- $ROOT/Users/alice/.local/share/containers/tainer"
	check "connections are edited as alice" logged "sudo -u #7001 -H $FAKE/plutil -remove"
	check "system files are not removed through sudo" not_logged "sudo -u #7001 -H rm -rf -- $ROOT/etc"
	teardown
}

test_path_is_pinned() {
	setup
	populate
	mkdir -p "$WORK/evil"
	printf '#!/bin/sh\ntouch %s/evil-ran\n' "$WORK" >"$WORK/evil/rm"
	cp "$WORK/evil/rm" "$WORK/evil/awk"
	chmod +x "$WORK/evil/rm" "$WORK/evil/awk"
	OUT=$(PATH="$WORK/evil:$PATH" LEGACY_CLEANUP_ROOT="$ROOT" LEGACY_USERS="$USERS_SPEC" \
		LAUNCHCTL="$FAKE/launchctl" PS="$FAKE/ps" KILL="$FAKE/kill" PKGUTIL="$FAKE/pkgutil" \
		PLUTIL="$FAKE/plutil" SUDO="$FAKE/sudo" bash "$SCRIPT" 2>&1)
	check "an rm or awk earlier on PATH is never run" [ ! -e "$WORK/evil-ran" ]
	check "the real rm still removed the helper" absent /Library/LaunchDaemons/com.github.containers.tainer.helper-alice.plist
	teardown
}

test_overrides_ignored_outside_sandbox() {
	local got
	got=$(LAUNCHCTL=/tmp/evil PS=/tmp/evil KILL=/tmp/evil SUDO=/tmp/evil PLUTIL=/tmp/evil BREW=/tmp/evil \
		bash -c 'source "$1"; printf "%s %s %s %s %s [%s]" "$LAUNCHCTL" "$PS" "$KILL" "$SUDO" "$PLUTIL" "$BREW"' _ "$SCRIPT")
	check "overrides are ignored without a sandbox root" \
		[ "$got" = "/bin/launchctl /bin/ps /bin/kill /usr/bin/sudo /usr/bin/plutil []" ]
	got=$(LEGACY_CLEANUP_ROOT=/tmp/x bash -c 'source "$1"; printf "%s %s %s" "$LAUNCHCTL" "$KILL" "$SUDO"' _ "$SCRIPT")
	check "the sandbox defaults to commands that do nothing" [ "$got" = "true true false" ]
}

test_unreadable_podman_helper_is_kept() {
	setup
	file /Library/LaunchDaemons/com.github.containers.podman.helper-gina.plist "bplist00 binary data"
	file /usr/local/podman/helper/gina/podman-mac-helper
	run --remove-podman-data
	check "an unreadable podman helper is kept, even with the flag" present /Library/LaunchDaemons/com.github.containers.podman.helper-gina.plist
	check "it isn't booted out" not_booted_out "helper-gina"
	check "says why" said "can't tell whose helper it is"
	teardown
}

test_remove_podman_data_refused_while_podman_is_installed() {
	setup
	populate
	file /opt/podman/bin/podman
	chmod +x "$ROOT/opt/podman/bin/podman"
	local before
	before=$(snapshot)
	run --remove-podman-data
	check "refused: exits 1" [ "$STATUS" -eq 1 ]
	check "refused: says why" said "--remove-podman-data was refused"
	check "refused: changes nothing" [ "$before" = "$(snapshot)" ]
	run
	check "without the flag: podman's helper kept" present /Library/LaunchDaemons/com.github.containers.podman.helper-bob.plist
	check "without the flag: podman's VM data kept" present /Users/alice/.local/share/containers/podman/machine/machine.pub
	check "without the flag: the old tainer still goes" absent /Users/alice/.local/share/containers/tainer
	teardown
}

test_piped_runs() {
	setup
	populate
	OUT=$(LEGACY_CLEANUP_ROOT="$ROOT" LEGACY_USERS="$USERS_SPEC" PS="$FAKE/ps" PKGUTIL="$FAKE/pkgutil" \
		PLUTIL="$FAKE/plutil" SUDO="$FAKE/sudo" bash -s -- --dry-run <"$SCRIPT" 2>&1)
	check "piped into bash: runs" said "Looking for the old tainer"
	check "piped into bash: honours options" said "would remove: $ROOT/Library/LaunchDaemons/com.github.containers.tainer.helper-alice.plist"
	OUT=$(LEGACY_CLEANUP_ROOT="$ROOT" LEGACY_UID=501 bash -s <"$SCRIPT" 2>&1)
	check "piped without root: explains sudo" said "| sudo bash"
	check "piped without root: removes nothing" present /Library/LaunchDaemons/com.github.containers.tainer.helper-alice.plist
	printf '#!/bin/bash\necho "sudo $*" >> %s\n' "$LOG" >"$WORK/bin/sudo-log"
	chmod +x "$WORK/bin/sudo-log"
	# Re-run only a file (and folder) owned by the person running it or by root.
	local me copy
	me=$(id -u)
	[ "$me" -eq 0 ] && me=4242 # as root, the copies below stay root-owned
	copy="$WORK/mine/legacy-cleanup.sh"
	mkdir -p "$WORK/mine" && cp "$SCRIPT" "$copy"
	OUT=$(LEGACY_CLEANUP_ROOT="$ROOT" LEGACY_UID="$me" SUDO="$WORK/bin/sudo-log" bash "$copy" --dry-run 2>&1)
	check "run from your own file without root: re-runs itself through sudo with its options" logged "sudo bash $copy --dry-run"
	: >"$LOG"
	copy="$WORK/theirs/legacy-cleanup.sh"
	mkdir -p "$WORK/theirs" && cp "$SCRIPT" "$copy"
	[ "$(id -u)" -eq 0 ] && chown -R 4343 "$WORK/theirs"
	OUT=$(LEGACY_CLEANUP_ROOT="$ROOT" LEGACY_UID=4242 SUDO="$WORK/bin/sudo-log" bash "$copy" --dry-run 2>&1)
	check "someone else's file: not re-run through sudo" not_logged "sudo bash"
	check "someone else's file: says to run it with sudo" said "sudo bash $copy"
	teardown
}

test_parse_home() {
	local a b
	a=$(printf 'NFSHomeDirectory: /Users/alice\n' | bash -c 'source "$1"; parse_home' _ "$SCRIPT")
	b=$(printf 'NFSHomeDirectory:\n /Users/Mary Ann\n' | bash -c 'source "$1"; parse_home' _ "$SCRIPT")
	check "home on the same line" [ "$a" = /Users/alice ]
	check "home with a space, on the next line" [ "$b" = "/Users/Mary Ann" ]
}

test_pf_conf_swapped_whole() {
	setup
	populate
	run
	check "no temporary pf.conf left behind" [ -z "$(ls "$ROOT"/etc/pf.conf.legacy-cleanup.* 2>/dev/null)" ]
	check "pf.conf keeps its other lines in order" [ "$(head -1 "$ROOT/etc/pf.conf")" = 'scrub-anchor "com.apple/*"' ]
	teardown
}

test_pf_swap_aborts_when_permissions_fail() {
	setup
	file /etc/pf.conf 'anchor "com.apple/*"
rdr-anchor "tainer"
'
	local before rc
	before=$(cksum <"$ROOT/etc/pf.conf")
	bash -c 'source "$1"; mode_owner() { echo "not-a-mode 0:0"; }; rewrite_pf_conf "$2"' _ "$SCRIPT" "$ROOT/etc/pf.conf" 2>/dev/null
	rc=$?
	check "a failed chmod aborts the swap" [ "$rc" -ne 0 ]
	check "a failed chmod leaves pf.conf as it was" [ "$before" = "$(cksum <"$ROOT/etc/pf.conf")" ]
	check "a failed chmod leaves no temporary file" [ -z "$(ls "$ROOT"/etc/pf.conf.legacy-cleanup.* 2>/dev/null)" ]
	teardown
}

test_orphaned_helper_sockets() {
	setup
	file /private/var/run/podman-helper-zed.socket
	file /private/var/run/tainer-helper-old.socket
	helper_plist /Library/LaunchDaemons/com.github.containers.podman.helper-bob.plist \
		com.github.containers.podman.helper-bob /usr/local/podman/helper/bob/podman-mac-helper
	file /usr/local/podman/helper/bob/podman-mac-helper
	file /private/var/run/podman-helper-bob.socket
	run
	check "an orphaned podman-name helper socket is swept" absent /private/var/run/podman-helper-zed.socket
	check "an orphaned tainer helper socket is swept" absent /private/var/run/tainer-helper-old.socket
	check "the socket of a kept podman helper stays" present /private/var/run/podman-helper-bob.socket
	teardown
}

test_brew_owner_unknown_is_reported() {
	setup
	file /Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist
	echo socket_vmnet >"$WORK/formulas"
	OUT=$(LEGACY_CLEANUP_ROOT="$ROOT" LEGACY_USERS="$USERS_SPEC" LAUNCHCTL="$FAKE/launchctl" \
		SUDO="$FAKE/sudo" BREW="$FAKE/brew" BREW_USER="#0" bash "$SCRIPT" 2>&1)
	check "root-owned Homebrew: never runs brew as root" not_logged "brew uninstall"
	check "root-owned Homebrew: says socket_vmnet may remain" said "socket_vmnet may still be installed with Homebrew"
	teardown
}

test_helper_binary_rules() {
	setup
	# A fork-labelled plist naming something that isn't the helper, or not where it lives.
	helper_plist /Library/LaunchDaemons/com.github.containers.tainer.helper-hal.plist \
		com.github.containers.tainer.helper-hal /usr/local/bin/important-tool
	file /usr/local/bin/important-tool
	helper_plist /Library/LaunchDaemons/com.github.containers.tainer.helper-ivy.plist \
		com.github.containers.tainer.helper-ivy /opt/elsewhere/tainer-mac-helper
	file /opt/elsewhere/tainer-mac-helper
	# The pre-0.1.8 helper next to another person's upstream helper folder.
	helper_plist /Library/LaunchDaemons/com.github.containers.podman.helper-jo.plist \
		com.github.containers.podman.helper-jo /usr/local/podman/helper/jo/tainer-mac-helper
	file /usr/local/podman/helper/jo/tainer-mac-helper
	mkdir -p "$ROOT/usr/local/podman/helper/kim"
	run
	check "a non-helper binary is kept" present /usr/local/bin/important-tool
	check "a helper outside its folder is kept" present /opt/elsewhere/tainer-mac-helper
	check "the plists themselves go" absent /Library/LaunchDaemons/com.github.containers.tainer.helper-hal.plist
	check "the fork helper binary goes" absent /usr/local/podman/helper/jo
	check "folders climb no further than the helper's own" present /usr/local/podman/helper/kim
	check "/usr/local/bin stays" present /usr/local/bin
	teardown
}

test_launch_agents_booted_out_per_person() {
	setup
	helper_plist /Library/LaunchAgents/com.github.containers.tainer.helper.plist \
		com.github.containers.tainer.helper /usr/local/tainer/helper/alice/tainer-mac-helper
	run
	check "agent booted out of alice's session" logged "launchctl bootout gui/7001/com.github.containers.tainer.helper"
	check "agent booted out of bob's session" logged "launchctl bootout gui/7002/com.github.containers.tainer.helper"
	check "agent not booted out of system" not_logged "launchctl bootout system/com.github.containers.tainer.helper"
	check "agent plist removed" absent /Library/LaunchAgents/com.github.containers.tainer.helper.plist
	teardown
}

test_unknown_option() {
	setup
	run --frobnicate
	check "unknown option exits 2" [ "$STATUS" -eq 2 ]
	check "unknown option is named" said "unknown option: --frobnicate"
	teardown
}

for t in test_dry_run_changes_nothing test_removes_the_old_tainer test_second_run_finds_nothing \
	test_clean_mac_finds_nothing test_helper_plists_matched_per_user test_remove_podman_data_flag \
	test_podman_installed_keeps_its_leftovers test_socket_vmnet_kept_for_lima test_old_receipt_forgotten \
	test_fork_opt_tainer_removed test_one_fork_marker_is_enough test_opt_tainer_kept_unless_positively_the_fork \
	test_never_follows_links_out_of_a_home test_home_files_removed_as_their_owner test_path_is_pinned \
	test_overrides_ignored_outside_sandbox test_unreadable_podman_helper_is_kept \
	test_remove_podman_data_refused_while_podman_is_installed test_piped_runs test_parse_home \
	test_pf_conf_swapped_whole test_pf_swap_aborts_when_permissions_fail test_orphaned_helper_sockets \
	test_brew_owner_unknown_is_reported test_helper_binary_rules \
	test_launch_agents_booted_out_per_person test_unknown_option; do
	before=$FAILS
	"$t"
	if [ "$FAILS" -eq "$before" ]; then echo "ok   $t"; else echo "FAIL $t"; fi
done
echo "$PASSES checks passed, $FAILS failed (bash $BASH_VERSION)"
[ "$FAILS" -eq 0 ]
