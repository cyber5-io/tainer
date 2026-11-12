#!/bin/bash
#
# tainer-uninstall — completely remove a tainer install (CLI + engine +
# menu app + VM helpers + the pf/sudoers changes) so a fresh tainer 1.0.0
# install lands on a clean machine. Covers both the current install and
# the legacy podman-fork tainer's leftovers.
#
#   sudo ./tainer-uninstall.sh           # remove tainer, KEEP user state
#   sudo ./tainer-uninstall.sh --purge   # ALSO delete ~/.cyberstack and
#                                         # ~/.config/tainer (projects, keys,
#                                         # certs, VM disks) — full reset
#
# Idempotent: safe to run when tainer isn't installed. Only touches
# tainer/cyberstack-owned paths.
set -u

# clean_launch_items ROOT CONSOLE_UID — boot out and delete the launchd
# items tainer ever installed, under ROOT ("" for the real system):
#   - the pre-1.6.0 menu agent, /Library/LaunchAgents/io.cyber5.tainer.menu.plist
#     (the menu is now its own login item, removed above);
#   - io.cyber5.tainer*, io.cyber5.cyberstack* and tainer-* items in
#     /Library/LaunchAgents and /Library/LaunchDaemons;
#   - the podman-fork era helper, a LaunchDaemon named
#     com.github.containers.tainer.helper-<user>.plist, and its socket.
# Items of other products are left alone, including upstream Podman's
# com.github.containers.podman.helper-<user> and Homebrew's socket_vmnet.
# Each item is booted out by the Label inside its plist (the file name when
# that can't be read). LAUNCHCTL overrides the launchctl binary (tests).
plist_label() {
    local label
    label=$(/usr/libexec/PlistBuddy -c 'Print :Label' "$1" 2>/dev/null) || label=""
    [ -n "$label" ] || label=$(basename "$1" .plist)
    printf '%s\n' "$label"
}

clean_launch_items() {
    local root="$1" uid="$2" lc="${LAUNCHCTL:-launchctl}" f label
    [ -n "$uid" ] && "$lc" bootout "gui/${uid}/io.cyber5.tainer.menu" 2>/dev/null
    for f in "$root"/Library/LaunchAgents/io.cyber5.tainer*.plist \
             "$root"/Library/LaunchAgents/io.cyber5.cyberstack*.plist \
             "$root"/Library/LaunchAgents/tainer-*.plist; do
        [ -f "$f" ] || continue
        label=$(plist_label "$f")
        [ -n "$uid" ] && "$lc" bootout "gui/${uid}/${label}" 2>/dev/null
        rm -f "$f" && echo "    removed $f"
    done
    for f in "$root"/Library/LaunchDaemons/io.cyber5.tainer*.plist \
             "$root"/Library/LaunchDaemons/io.cyber5.cyberstack*.plist \
             "$root"/Library/LaunchDaemons/tainer-*.plist \
             "$root"/Library/LaunchDaemons/com.github.containers.tainer.helper-*.plist; do
        [ -f "$f" ] || continue
        label=$(plist_label "$f")
        "$lc" bootout "system/${label}" 2>/dev/null
        rm -f "$f" && echo "    removed $f"
    done
    rm -f "$root"/private/var/run/tainer-helper-*.socket
    return 0
}

main() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "must run as root: sudo $0 [--purge]" >&2
        exit 1
    fi

    PURGE=0
    [ "${1:-}" = "--purge" ] && PURGE=1

    CONSOLE_USER=$(stat -f%Su /dev/console 2>/dev/null)
    CONSOLE_UID=$(id -u "$CONSOLE_USER" 2>/dev/null || true)
    CONSOLE_HOME=$(dscl . -read "/Users/$CONSOLE_USER" NFSHomeDirectory 2>/dev/null | awk '{print $2}')
    [ -z "${CONSOLE_HOME:-}" ] && CONSOLE_HOME="/Users/$CONSOLE_USER"

    echo "==> removing the menu's login item"
    MENU_BIN="/Applications/Tainer Menu.app/Contents/MacOS/TainerMenu"
    if [ -x "$MENU_BIN" ] && [ -n "${CONSOLE_UID:-}" ] && [ "$CONSOLE_USER" != "root" ]; then
        launchctl asuser "$CONSOLE_UID" sudo -u "$CONSOLE_USER" "$MENU_BIN" --unregister-login-item 2>/dev/null || true
    fi
    pkill -TERM -x TainerMenu 2>/dev/null || true

    echo "==> unloading launch agents and daemons"
    clean_launch_items "" "${CONSOLE_UID:-}"

    echo "==> stopping cyberstackd + VM/network helpers"
    pkill -x cyberstackd  2>/dev/null || true
    pkill -f cyberstack-pf 2>/dev/null || true
    pkill -x gvproxy      2>/dev/null || true
    pkill -x vfkit        2>/dev/null || true
    pkill -x vmnet-helper 2>/dev/null || true
    sleep 1

    echo "==> reverting /etc/pf.conf + removing sudoers"
    if grep -q 'cyberstack/\*' /etc/pf.conf 2>/dev/null; then
        sed -i '' '/cyberstack\/\*/d' /etc/pf.conf
        pfctl -f /etc/pf.conf 2>/dev/null || true
    fi
    rm -f /etc/pf.conf.cyberstack.bak
    rm -f /etc/sudoers.d/cyberstack /etc/sudoers.d/vmnet-helper

    echo "==> removing installed files"
    rm -rf /opt/tainer /opt/vmnet-helper
    rm -f  /usr/local/bin/tainer
    rm -rf "/Applications/Tainer Menu.app"

    echo "==> forgetting package receipts"
    for p in io.cyber5.tainer io.cyber5.tainer.only io.cyber5.tainer.full io.cyber5.cyberstack; do
        pkgutil --forget "$p" >/dev/null 2>&1 || true
    done

    echo "==> removing tainer SSH client config"
    # Remove tainer SSH client config (both drop-ins + the ~/.ssh/config include).
    rm -f /etc/ssh/ssh_config.d/0-tainer.conf
    rm -f /etc/resolver/tainer.me
    rm -f "$CONSOLE_HOME/.cyberstack/0-tainer.conf"
    SSH_CFG="$CONSOLE_HOME/.ssh/config"
    if [ -f "$SSH_CFG" ]; then
        grep -vF 'Include ~/.cyberstack/0-tainer.conf' "$SSH_CFG" > "$SSH_CFG.tainer-tmp" 2>/dev/null \
            && mv "$SSH_CFG.tainer-tmp" "$SSH_CFG"
    fi

    if [ "$PURGE" -eq 1 ]; then
        echo "==> purging user state under $CONSOLE_HOME"
        rm -rf "$CONSOLE_HOME/.cyberstack" "$CONSOLE_HOME/.config/tainer"
    else
        echo "==> kept user state: $CONSOLE_HOME/.cyberstack, $CONSOLE_HOME/.config/tainer"
    fi

    echo "==> tainer uninstalled cleanly."
    [ "$PURGE" -eq 0 ] && echo "    (re-run with --purge to also wipe projects/keys/certs/VM disks)"
}

# Sourced (tests) defines the functions only; run, it uninstalls.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
