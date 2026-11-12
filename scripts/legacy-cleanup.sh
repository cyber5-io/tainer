#!/bin/bash
#
# legacy-cleanup.sh — remove the old tainer (the 0.x releases, built on podman)
# and the tools it relied on from this Mac. Safe on a Mac that already runs the
# current tainer: it never touches anything the current tainer uses.
#
#   sudo bash legacy-cleanup.sh                 # remove what it finds
#   sudo bash legacy-cleanup.sh --dry-run       # only list what it would remove
#   curl -fsSL <url> | sudo bash                # the same, straight from a download
#   curl -fsSL <url> | sudo bash -s -- --dry-run
#
#   --remove-podman-data    also remove podman's own machine data and installer
#                           leftovers (kept by default; refused while podman is
#                           installed, because that podman would lose its data)
#   --remove-socket-vmnet   remove Homebrew's socket_vmnet even when Lima or
#                           Colima is installed
#
# Running it twice is fine: the second run finds nothing to do.
#
# NEVER TOUCHED (the current tainer owns these):
#   /opt/tainer (*)                  ~/.config/tainer
#   ~/.cyberstack                    /Applications/Tainer Menu.app
#   /etc/resolver/tainer.me          /usr/local/bin/tainer
#   io.cyber5.* launch items and package receipts of tainer 1.0 or later
#
# (*) /opt/tainer is removed only when it is positively the old tainer's: there
#     is no /opt/tainer/bin/cyberstackd, at least one file only the old tainer
#     installed is there (bin/tainer-mac-helper, bin/krunkit, lib/libkrun-efi.dylib,
#     certs/, docs/man, bin/uninstall.sh), and nothing only the current tainer
#     installs is (bin/cyberstack-pf, bin/tainer-uninstall.sh, share/cyberstack,
#     share/tainer). A missing cyberstackd alone never counts.
#
# Files inside a person's home folder are removed as that person, never as root,
# and only when they really are inside that home folder (no links leading out).
#
# Works with the bash that ships with macOS (3.2).
set -u
PATH=/usr/bin:/bin:/usr/sbin:/sbin
export PATH

# LEGACY_CLEANUP_ROOT puts the script in sandbox mode for its tests: every path
# is taken under that folder, and the system commands below can be swapped for
# fakes. Outside sandbox mode none of these overrides is honoured.
ROOT="${LEGACY_CLEANUP_ROOT:-}"
if [ -n "$ROOT" ]; then
	LAUNCHCTL="${LAUNCHCTL:-true}"
	PS="${PS:-true}"
	KILL="${KILL:-true}"
	PFCTL="${PFCTL:-true}"
	PKGUTIL="${PKGUTIL:-false}"
	PLUTIL="${PLUTIL:-false}"
	PLISTBUDDY="${PLISTBUDDY:-}"
	SUDO="${SUDO:-false}"
	BREW="${BREW:-}"
	BREW_USER="${BREW_USER:-}"
	MY_UID="${LEGACY_UID:-0}"
else
	LAUNCHCTL=/bin/launchctl
	PS=/bin/ps
	KILL=/bin/kill
	PFCTL=/sbin/pfctl
	PKGUTIL=/usr/sbin/pkgutil
	PLUTIL=/usr/bin/plutil
	PLISTBUDDY=/usr/libexec/PlistBuddy
	SUDO=/usr/bin/sudo
	BREW=""
	BREW_USER=""
	MY_UID=$(id -u)
fi

DRY=0
REMOVE_PODMAN=0
REMOVE_VMNET=0
COUNT=0
KEPT=""
DONE="
" # paths already handled, so a dry run lists each one once

usage() {
	cat <<'USAGE'
Removes the old tainer (0.x, built on podman) and the tools it relied on.

  sudo bash legacy-cleanup.sh                 remove what it finds
  sudo bash legacy-cleanup.sh --dry-run       only list what it would remove
  curl -fsSL <url> | sudo bash -s -- [options]

  --dry-run, -n           list everything, change nothing
  --remove-podman-data    also remove podman's own machine data and installer
                          leftovers (refused while podman is installed)
  --remove-socket-vmnet   remove Homebrew's socket_vmnet even when Lima or
                          Colima is installed

The current tainer is never touched. Running it twice is fine.
USAGE
}

p() { printf '%s%s' "$ROOT" "$1"; }

say() { printf '%s\n' "$*"; }

keep() {
	KEPT="${KEPT}  - $*
"
}

exists() { [ -e "$1" ] || [ -L "$1" ]; }

# handled PATH: true if PATH was already removed (or listed) in this run; marks it otherwise.
handled() {
	case "$DONE" in *"
$1
"*) return 0 ;;
	esac
	DONE="$DONE$1
"
	return 1
}

# act DESCRIPTION COMMAND... : run (or, with --dry-run, only announce) one removal.
act() {
	local what="$1"
	shift
	COUNT=$((COUNT + 1))
	if [ "$DRY" -eq 1 ]; then
		say "would remove: $what"
		return 0
	fi
	if "$@"; then
		say "removed: $what"
	else
		say "could not remove: $what"
	fi
}

# remove_path PATH [NOTE]: delete a file, link or folder outside any home folder.
remove_path() {
	exists "$1" || return 0
	handled "$1" && return 0
	act "$1${2:+ ($2)}" rm -rf -- "$1"
}

# run_as WHO COMMAND...: run a command as another person. WHO is "#<uid>", which sudo
# takes as that exact account (a name could be re-pointed; a uid can't).
run_as() {
	local who="$1"
	shift
	if [ "$who" = "#$(id -u)" ]; then
		"$@"
	else
		"$SUDO" -u "$who" -H "$@"
	fi
}

# real_dir DIR: DIR with every link resolved, or nothing if it does not exist.
real_dir() { (cd -P -- "$1" 2>/dev/null && pwd -P); }

# inside_home PATH HOME: true when PATH's folder, links resolved, is HOME or below it.
inside_home() {
	local dir home
	dir=$(real_dir "$(dirname -- "$1")") || return 1
	home=$(real_dir "$2") || return 1
	[ -n "$dir" ] && [ -n "$home" ] || return 1
	case "$dir/" in "$home"/*) return 0 ;; esac
	return 1
}

# remove_user_path UID HOME PATH [NOTE]: delete something in a home folder, as its
# owner, and only if it is really inside that home folder.
remove_user_path() {
	local uid="$1" home="$2" path="$3"
	exists "$path" || return 0
	handled "$path" && return 0
	# Only the folders above PATH can lead elsewhere: rm -rf on a link removes the link.
	if ! inside_home "$path" "$home"; then
		keep "$path — it leads outside $home, so it was left alone"
		return 0
	fi
	act "$path${4:+ ($4)}" run_as "#$uid" rm -rf -- "$path"
}

size_of() { du -sh -- "$1" 2>/dev/null | awk '{print $1}'; }

# plist_string FILE KEYPATH: one value from a launchd plist (Label, ProgramArguments:0).
# Empty when it can't be read, which callers treat as "cannot tell".
plist_string() {
	local v=""
	if [ -n "$PLISTBUDDY" ] && [ -x "$PLISTBUDDY" ]; then
		v=$("$PLISTBUDDY" -c "Print :$2" "$1" 2>/dev/null) || v=""
	fi
	if [ -z "$v" ]; then
		v=$(awk -v key="<key>${2%%:*}</key>" '
			index($0, key) { found = 1; next }
			found && /<string>/ {
				sub(/.*<string>/, ""); sub(/<\/string>.*/, ""); print; exit
			}' "$1" 2>/dev/null)
	fi
	printf '%s' "$v"
}

# parse_home: the NFSHomeDirectory value from `dscl . -read /Users/x NFSHomeDirectory`,
# which may sit on the same line or, when it has spaces, on the next one.
parse_home() {
	sed '1s/^NFSHomeDirectory://' | tr -d '\n' | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

# users: "name uid home" for each person with a home folder on this Mac.
users() {
	if [ -n "$ROOT" ]; then
		local entry
		for entry in ${LEGACY_USERS:-}; do
			printf '%s %s %s\n' "${entry%%:*}" "${entry##*:}" "$ROOT/Users/${entry%%:*}"
		done
		return 0
	fi
	local name uid home
	dscl . -list /Users UniqueID 2>/dev/null | while read -r name uid; do
		case "$name" in _* | root | daemon | nobody) continue ;; esac
		[ "$uid" -ge 500 ] 2>/dev/null || continue
		home=$(dscl . -read "/Users/$name" NFSHomeDirectory 2>/dev/null | parse_home)
		[ -d "$home" ] && printf '%s %s %s\n' "$name" "$uid" "$home"
	done
}

user_tmp() { # user_tmp NAME UID
	if [ -n "$ROOT" ]; then
		printf '%s' "$ROOT/private/var/folders/$1/T"
	else
		run_as "#$2" getconf DARWIN_USER_TEMP_DIR 2>/dev/null | sed 's:/*$::'
	fi
}

podman_installed() {
	local b
	for b in /opt/podman/bin/podman /opt/homebrew/bin/podman /usr/local/bin/podman; do
		[ -x "$(p "$b")" ] && return 0
	done
	return 1
}

# ---------------------------------------------------------------------------

# prune_helper_dir BINARY: remove the helper's own empty folders, and no others.
prune_helper_dir() {
	[ "$DRY" -eq 1 ] && return 0
	local dir
	dir=$(dirname "$1")
	case "$dir" in
	"$(p /usr/local/tainer/helper)"/*)
		rmdir "$dir" 2>/dev/null && rmdir "$(p /usr/local/tainer/helper)" 2>/dev/null &&
			rmdir "$(p /usr/local/tainer)" 2>/dev/null
		;;
	"$(p /usr/local/podman/helper)"/*) rmdir "$dir" 2>/dev/null ;;
	esac
	return 0
}

# is_fork_helper_binary PATH: tainer-mac-helper in the fork's helper folders.
is_fork_helper_binary() {
	[ "$(basename "$1")" = tainer-mac-helper ] || return 1
	case "$1" in
	"$(p /usr/local/tainer/helper)"/*/tainer-mac-helper | "$(p /usr/local/podman/helper)"/*/tainer-mac-helper) return 0 ;;
	esac
	return 1
}

bootout() { # bootout DOMAIN/LABEL
	if [ "$DRY" -eq 1 ]; then
		say "would stop: ${1##*/}"
	else
		"$LAUNCHCTL" bootout "$1" >/dev/null 2>&1 || true
	fi
}

# The fork's docker.sock helper: com.github.containers.tainer.helper-<user> (0.1.8
# and later) and, before 0.1.8, the same helper under podman's name. Upstream
# podman's own helper runs podman-mac-helper; the fork's always ran
# tainer-mac-helper, which is how the two are told apart. A helper whose program
# can't be read is upstream podman's until shown otherwise, so it is kept.
clean_helpers() {
	local f label prog user kind name uid home
	for f in "$(p /Library/LaunchDaemons)"/com.github.containers.tainer.helper-*.plist \
		"$(p /Library/LaunchDaemons)"/com.github.containers.podman.helper-*.plist; do
		[ -f "$f" ] || continue
		label=$(plist_string "$f" Label)
		[ -n "$label" ] || label=$(basename "$f" .plist)
		prog=$(plist_string "$f" ProgramArguments:0)
		user=${label##*.helper-}
		case "$label" in
		com.github.containers.podman.helper-*)
			kind=podman
			if [ "$(basename "$prog")" != tainer-mac-helper ]; then
				if [ -z "$prog" ]; then
					keep "$f — can't tell whose helper it is, so it was left alone"
					continue
				fi
				if podman_installed || { [ "$REMOVE_PODMAN" -eq 0 ] && exists "$(p "$prog")"; }; then
					keep "podman's docker.sock helper ($f) — pass --remove-podman-data to remove it"
					continue
				fi
			fi
			;;
		*) kind=tainer ;;
		esac
		bootout "system/$label"
		act "$f" rm -f -- "$f"
		remove_path "$(p "/private/var/run/$kind-helper-$user.socket")"
		if [ -n "$prog" ] && exists "$(p "$prog")" && ! handled "$(p "$prog")"; then
			if is_fork_helper_binary "$(p "$prog")" ||
				{ [ "$kind" = podman ] && [ "$(basename "$prog")" = podman-mac-helper ] &&
					case "$(p "$prog")" in "$(p /usr/local/podman/helper)"/*) true ;; *) false ;; esac; }; then
				act "$(p "$prog")" rm -f -- "$(p "$prog")"
				prune_helper_dir "$(p "$prog")"
			else
				keep "$(p "$prog") — not where the helper is installed, so it was left alone"
			fi
		fi
	done
	# An agent copy some versions left in /Library/LaunchAgents: per person, in gui/<uid>.
	for f in "$(p /Library/LaunchAgents)"/com.github.containers.tainer.helper*.plist; do
		[ -f "$f" ] || continue
		label=$(plist_string "$f" Label)
		[ -n "$label" ] || label=$(basename "$f" .plist)
		while read -r name uid home; do
			[ -n "$name" ] && bootout "gui/$uid/$label"
		done <<EOF
$(users)
EOF
		act "$f" rm -f -- "$f"
	done
	# Helper sockets whose launch item is already gone. podman's name is swept only
	# when no podman helper plist for that person is left (a kept one owns it).
	for f in "$(p /private/var/run)"/tainer-helper-*.socket "$(p /private/var/run)"/podman-helper-*.socket; do
		exists "$f" || continue
		case "$(basename "$f")" in
		podman-helper-*)
			user=$(basename "$f" .socket)
			user=${user#podman-helper-}
			[ -f "$(p "/Library/LaunchDaemons/com.github.containers.podman.helper-$user.plist")" ] && continue
			;;
		esac
		remove_path "$f"
	done
	# Helper binaries whose launch item is already gone.
	for f in "$(p /usr/local/tainer/helper)"/*/tainer-mac-helper \
		"$(p /usr/local/podman/helper)"/*/tainer-mac-helper; do
		[ -f "$f" ] || continue
		handled "$f" && continue
		act "$f" rm -f -- "$f"
		prune_helper_dir "$f"
	done
}

# The fork's processes: its helper, and vfkit, krunkit or gvproxy running its VM
# (a path under containers/tainer/machine or the name tainer-machine-default in
# their arguments). Matched by program name and argument, never by a bare
# pattern, so an editor with one of those files open is left alone. SIGTERM only.
fork_processes() {
	"$PS" -axww -o pid= -o command= 2>/dev/null | awk -v self="$$" '
		{
			pid = $1; prog = $2; n = split(prog, part, "/"); name = part[n]
			if (pid == self) next
			if (name == "tainer-mac-helper") { print; next }
			if (name != "vfkit" && name != "krunkit" && name != "gvproxy") next
			if (index($0, "containers/tainer/machine") || index($0, "tainer-machine-default")) print
		}'
}

stop_processes() {
	local lines pids
	lines=$(fork_processes)
	[ -n "$lines" ] || return 0
	COUNT=$((COUNT + 1))
	if [ "$DRY" -eq 1 ]; then
		say "would stop these old tainer processes:"
		printf '%s\n' "$lines" | sed 's/^ */    /'
		return 0
	fi
	pids=$(printf '%s\n' "$lines" | awk '{print $1}')
	# shellcheck disable=SC2086 # one pid per word
	"$KILL" -TERM $pids 2>/dev/null || true
	local i=0
	while [ $i -lt 10 ] && [ -n "$(fork_processes)" ]; do
		sleep 1
		i=$((i + 1))
	done
	if [ -n "$(fork_processes)" ]; then
		say "still running: some old tainer processes did not stop; restart the Mac to finish."
	else
		say "stopped: old tainer processes ($(echo "$pids" | tr '\n' ' ' | sed 's/ $//'))"
	fi
}

# stat: macOS takes -f FORMAT; GNU (the tests' Linux runs) takes -c, and its -f means
# something else, so the flavour is picked once rather than tried in turn.
if stat -c %u / >/dev/null 2>&1; then
	stat_fmt() { stat -c "$2" "$1" 2>/dev/null; } # stat_fmt FILE GNU-FORMAT [BSD-FORMAT]
else
	stat_fmt() { stat -f "${3:-$2}" "$1" 2>/dev/null; }
fi

mode_owner() { stat_fmt "$1" '%a %u:%g' '%Lp %u:%g'; }

# /etc/pf.conf: the fork added 'rdr-anchor "tainer"' and a 'load anchor "tainer"' line.
# Rewritten through a checked temporary file and swapped in with one rename.
PF_LINES='^(rdr-anchor|load anchor) "tainer"'

rewrite_pf_conf() {
	local conf="$1" tmp before ours after mo
	tmp=$(mktemp "$conf.legacy-cleanup.XXXXXX") || return 1
	before=$(wc -l <"$conf")
	ours=$(grep -cE "$PF_LINES" "$conf")
	if ! grep -vE "$PF_LINES" "$conf" >"$tmp"; then
		rm -f "$tmp"
		return 1
	fi
	after=$(wc -l <"$tmp")
	if [ ! -s "$tmp" ] || [ "$after" -ne $((before - ours)) ]; then
		rm -f "$tmp"
		return 1
	fi
	mo=$(mode_owner "$conf")
	if [ -z "$mo" ] || ! chmod "${mo%% *}" "$tmp"; then
		rm -f "$tmp"
		return 1
	fi
	chown "${mo#* }" "$tmp" 2>/dev/null
	if ! mv -f "$tmp" "$conf"; then
		rm -f "$tmp"
		return 1
	fi
	[ -n "$ROOT" ] || "$PFCTL" -f /etc/pf.conf >/dev/null 2>&1 || true
}

clean_pf() {
	local conf
	conf=$(p /etc/pf.conf)
	if [ -f "$conf" ] && grep -qE "$PF_LINES" "$conf"; then
		act "the old tainer's lines in $conf" rewrite_pf_conf "$conf"
	fi
	remove_path "$(p /etc/pf.anchors/tainer)"
	remove_path "$(p /etc/pf.conf.tainer-backup)"
}

forget_receipt() { "$PKGUTIL" --forget "$1" >/dev/null 2>&1; }

clean_system() {
	remove_path "$(p /etc/paths.d/00-tainer)" "put /opt/tainer/bin on the PATH"
	remove_path "$(p /usr/local/etc/man.d/tainer.man.conf)"

	# krunkit's library path: /opt/podman/lib -> /opt/tainer/lib.
	local lib
	lib=$(p /opt/podman/lib)
	if [ -L "$lib" ] && [ "$(readlink "$lib")" = /opt/tainer/lib ]; then
		act "$lib (link to /opt/tainer/lib)" rm -f -- "$lib"
		[ "$DRY" -eq 1 ] || rmdir "$(p /opt/podman)" 2>/dev/null || true
	fi

	# /var/run/docker.sock, when it leads to the old tainer's VM.
	local sock target
	sock=$(p /var/run/docker.sock)
	if [ -L "$sock" ]; then
		target=$(readlink "$sock")
		case "$target" in
		*/containers/tainer/machine/*) act "$sock (pointed at the old tainer)" rm -f -- "$sock" ;;
		*/containers/podman/machine/*)
			if ! exists "$(p "$target")" && ! podman_installed; then
				act "$sock (pointed at a podman VM that is gone)" rm -f -- "$sock"
			fi
			;;
		esac
	fi

	# The fork's package receipt shares io.cyber5.tainer with the current tainer;
	# only a 0.x version is the fork's.
	local version
	version=$("$PKGUTIL" --pkg-info io.cyber5.tainer 2>/dev/null | awk '/^version:/ {print $2}')
	case "$version" in
	0.*) act "the installer receipt for tainer $version" forget_receipt io.cyber5.tainer ;;
	esac
}

# What only the old tainer put in /opt/tainer (contrib/pkginstaller/package.sh),
# and what only the current tainer puts there.
FORK_OPT_MARKERS="bin/tainer-mac-helper bin/krunkit lib/libkrun-efi.dylib certs docs/man bin/uninstall.sh"
CURRENT_OPT_MARKERS="bin/cyberstackd bin/cyberstack-pf bin/tainer-uninstall.sh share/cyberstack share/tainer"

# opt_tainer_is_fork: true only on positive identification of the old tainer's /opt/tainer.
opt_tainer_is_fork() {
	local dir m found=0
	dir=$(p /opt/tainer)
	[ -d "$dir" ] && [ ! -L "$dir" ] || return 1
	for m in $CURRENT_OPT_MARKERS; do
		exists "$dir/$m" && return 1
	done
	for m in $FORK_OPT_MARKERS; do
		exists "$dir/$m" && found=1
	done
	[ "$found" -eq 1 ]
}

clean_opt_tainer() {
	opt_tainer_is_fork || return 0
	remove_path "$(p /opt/tainer)" "the old tainer's programs"
}

# Per person: the fork's VM, its settings and its scratch folder. Everything here
# is in the person's own folders, so it is removed as that person.
clean_user() { # clean_user NAME UID HOME
	local name="$1" uid="$2" home="$3" d f tmp share conf
	share="$home/.local/share/containers"
	conf="$home/.config/containers"

	for d in "$share/tainer" "$conf/tainer"; do
		exists "$d" && remove_user_path "$uid" "$home" "$d" "$(size_of "$d")"
	done
	tmp=$(user_tmp "$name" "$uid")
	if [ -n "$tmp" ] && exists "$tmp/tainer"; then
		inside_home "$tmp/tainer" "$tmp" && ! [ -L "$tmp/tainer" ] &&
			act "$tmp/tainer" run_as "#$uid" rm -rf -- "$tmp/tainer"
	fi

	# Before 0.1.8 the fork kept its VM in podman's folders, as tainer-machine-default.
	for f in "$share"/podman/machine/*/tainer-machine-default* \
		"$conf"/podman/machine/*/tainer-machine-default*; do
		exists "$f" && remove_user_path "$uid" "$home" "$f"
	done

	# The fork's entries in podman's list of connections, edited as the person.
	local list="$conf/podman-connections.json" entry default
	if [ -f "$list" ] && ! [ -L "$list" ] && inside_home "$list" "$home" &&
		grep -q '"tainer-machine-default' "$list"; then
		default=$(run_as "#$uid" "$PLUTIL" -extract Connection.Default raw "$list" 2>/dev/null)
		for entry in tainer-machine-default tainer-machine-default-root; do
			run_as "#$uid" "$PLUTIL" -extract "Connection.Connections.$entry" json -o - "$list" >/dev/null 2>&1 || continue
			act "connection $entry in $list" run_as "#$uid" "$PLUTIL" -remove "Connection.Connections.$entry" "$list"
		done
		case "$default" in
		tainer-machine-default | tainer-machine-default-root)
			act "the default connection ($default) in $list" run_as "#$uid" "$PLUTIL" -remove Connection.Default "$list"
			;;
		esac
	fi

	# podman's own machine folders: kept unless asked.
	for d in "$share/podman" "$conf/podman"; do
		exists "$d" || continue
		if [ "$REMOVE_PODMAN" -eq 1 ]; then
			remove_user_path "$uid" "$home" "$d" "$(size_of "$d")"
		else
			keep "$d ($(size_of "$d")) — podman's machine data; pass --remove-podman-data to remove it"
		fi
	done
}

# Installer leftovers of upstream podman, only with --remove-podman-data (which is
# refused while podman is installed).
clean_podman_leftovers() {
	local any=0 f
	for f in /etc/paths.d/podman-pkg /usr/local/etc/man.d/podman.man.conf; do
		exists "$(p "$f")" && any=1
	done
	"$PKGUTIL" --pkg-info com.redhat.podman >/dev/null 2>&1 && any=1
	[ "$any" -eq 1 ] || return 0
	podman_installed && return 0
	if [ "$REMOVE_PODMAN" -eq 0 ]; then
		keep "podman's installer leftovers (/etc/paths.d/podman-pkg and similar) — pass --remove-podman-data to remove them"
		return 0
	fi
	remove_path "$(p /etc/paths.d/podman-pkg)"
	remove_path "$(p /usr/local/etc/man.d/podman.man.conf)"
	if "$PKGUTIL" --pkg-info com.redhat.podman >/dev/null 2>&1; then
		act "the installer receipt for podman" forget_receipt com.redhat.podman
	fi
	[ "$DRY" -eq 1 ] || rmdir "$(p /opt/podman)" 2>/dev/null || true
}

brew_bin() {
	local b
	for b in "$BREW" "$(p /opt/homebrew/bin/brew)" "$(p /usr/local/bin/brew)"; do
		[ -n "$b" ] && [ -x "$b" ] && {
			printf '%s' "$b"
			return 0
		}
	done
	return 1
}

owner_of() { stat_fmt "$1" %U %Su; }
uid_of() { stat_fmt "$1" %u; }

# socket_vmnet: a network helper an early test build of the current tainer
# asked people to install with Homebrew (`sudo brew services start socket_vmnet`).
clean_socket_vmnet() {
	local brew="" owner="" who="" usable=0 installed=0 lima=0 name uid home plist
	if brew=$(brew_bin); then
		# BREW_USER (sandbox only) gives the owner as "#<uid>".
		who=${BREW_USER:-"#$(uid_of "$brew")"}
		owner=$(owner_of "$brew")
		[ -n "$BREW_USER" ] && owner=$BREW_USER
		if [ "$who" != "#" ] && [ "$who" != "#0" ]; then
			usable=1
			run_as "$who" "$brew" list --formula socket_vmnet >/dev/null 2>&1 && installed=1
			if run_as "$who" "$brew" list --formula lima >/dev/null 2>&1 ||
				run_as "$who" "$brew" list --formula colima >/dev/null 2>&1; then
				lima=1
			fi
		fi
	fi
	while read -r name uid home; do
		[ -n "$name" ] || continue
		{ [ -d "$home/.lima" ] || [ -d "$home/.colima" ]; } && lima=1
	done <<EOF
$(users)
EOF

	plist=$(p /Library/LaunchDaemons/homebrew.mxcl.socket_vmnet.plist)
	if [ "$installed" -eq 0 ] && ! [ -f "$plist" ]; then
		return 0
	fi
	if [ "$lima" -eq 1 ] && [ "$REMOVE_VMNET" -eq 0 ]; then
		keep "socket_vmnet (Homebrew) — Lima or Colima uses it too; pass --remove-socket-vmnet to remove it"
		return 0
	fi
	if [ -f "$plist" ]; then
		bootout system/homebrew.mxcl.socket_vmnet
		act "$plist" rm -f -- "$plist"
	fi
	while read -r name uid home; do
		[ -n "$name" ] || continue
		plist="$home/Library/LaunchAgents/homebrew.mxcl.socket_vmnet.plist"
		[ -f "$plist" ] || continue
		bootout "gui/$uid/homebrew.mxcl.socket_vmnet"
		remove_user_path "$uid" "$home" "$plist"
	done <<EOF
$(users)
EOF
	if [ "$installed" -eq 1 ]; then
		act "socket_vmnet (Homebrew, uninstalled as $owner)" run_as "$who" "$brew" uninstall --formula socket_vmnet
	elif [ -n "$brew" ] && [ "$usable" -eq 0 ]; then
		[ "$who" = "#0" ] && owner=root
		keep "socket_vmnet may still be installed with Homebrew, which belongs to ${owner:-an unknown user}; run \`brew uninstall socket_vmnet\` as the person who installed Homebrew"
	fi
}

# ensure_root ARGS...: re-run with sudo when needed. A piped script can't be
# re-run, so it says how to pipe it into sudo instead.
ensure_root() {
	[ "$MY_UID" -eq 0 ] && return 0
	local self="${BASH_SOURCE[0]:-}" f u safe=1
	if [ -n "$self" ] && [ -f "$self" ] && [ -r "$self" ]; then
		# Only re-run a file that you or root own, in a folder you or root own,
		# so nobody else can swap it between this check and sudo reading it.
		for f in "$self" "$(dirname -- "$self")"; do
			u=$(uid_of "$f")
			[ "$u" = "$MY_UID" ] || [ "$u" = 0 ] || safe=0
		done
		if [ "$safe" -eq 1 ]; then
			say "This needs administrator rights to remove system files; asking for your password."
			exec "$SUDO" bash "$self" "$@"
		fi
		say "This needs administrator rights. Run it with sudo:"
		say "  sudo bash $self"
		exit 1
	fi
	say "This needs administrator rights. Run it through sudo, for example:"
	say "  curl -fsSL <url> | sudo bash"
	say "  curl -fsSL <url> | sudo bash -s -- --dry-run"
	exit 1
}

main() {
	case " $* " in *" -h "* | *" --help "*)
		usage
		return 0
		;;
	esac
	if [ -z "$ROOT" ] && [ "$(uname -s)" != Darwin ]; then
		say "This script is for macOS only."
		return 1
	fi
	ensure_root "$@"

	while [ $# -gt 0 ]; do
		case "$1" in
		--dry-run | -n) DRY=1 ;;
		--remove-podman-data) REMOVE_PODMAN=1 ;;
		--remove-socket-vmnet) REMOVE_VMNET=1 ;;
		*)
			say "unknown option: $1"
			usage
			return 2
			;;
		esac
		shift
	done

	if [ "$REMOVE_PODMAN" -eq 1 ] && podman_installed; then
		say "podman is installed on this Mac, so --remove-podman-data was refused: that podman"
		say "would lose its machines and data. Uninstall podman first, or run without the option."
		return 1
	fi

	if [ "$DRY" -eq 1 ]; then
		say "Looking for the old tainer and the tools it relied on (dry run: nothing is changed)."
	else
		say "Removing the old tainer and the tools it relied on."
	fi

	clean_helpers
	stop_processes
	clean_pf
	clean_system
	clean_opt_tainer
	local name uid home
	while read -r name uid home; do
		[ -n "$name" ] && clean_user "$name" "$uid" "$home"
	done <<EOF
$(users)
EOF
	clean_podman_leftovers
	clean_socket_vmnet

	say ""
	if [ "$COUNT" -eq 0 ]; then
		say "Nothing from the old tainer was found. This Mac is clean."
	elif [ "$DRY" -eq 1 ]; then
		say "Found $COUNT item(s) to remove. Run again without --dry-run to remove them."
	else
		say "Done: removed $COUNT item(s)."
	fi
	if [ -n "$KEPT" ]; then
		say "Kept, because they may belong to something else on this Mac:"
		printf '%s' "$KEPT"
	fi
	return 0
}

# Run unless sourced (the tests source it to reach the functions). When piped
# into bash there is no BASH_SOURCE, which also means "run".
if [ "${BASH_SOURCE[0]:-$0}" = "$0" ]; then
	main "$@"
	exit $?
fi
