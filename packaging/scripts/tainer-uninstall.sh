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

echo "==> unloading launch agents (menu + VM helpers)"
for label in io.cyber5.tainer.menu \
             com.github.containers.tainer.helper \
             com.github.containers.podman.helper; do
    [ -n "${CONSOLE_UID:-}" ] && launchctl bootout "gui/${CONSOLE_UID}/${label}" 2>/dev/null || true
    launchctl bootout "system/${label}" 2>/dev/null || true
done
rm -f /Library/LaunchAgents/io.cyber5.tainer.menu.plist
rm -f /Library/LaunchAgents/com.github.containers.tainer.helper-*.plist
rm -f /Library/LaunchAgents/com.github.containers.podman.helper-*.plist
rm -f /Library/LaunchDaemons/io.cyber5.*.plist

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
