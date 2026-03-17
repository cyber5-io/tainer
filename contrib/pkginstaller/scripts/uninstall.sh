#!/bin/bash
#
# Tainer uninstall script.
# Must be run as root: sudo bash /opt/tainer/bin/uninstall.sh
#
# Order matters: stop workloads first, then remove the machine,
# then clean up system config, and finally remove the binaries.

set -e

echo "Uninstalling Tainer..."

# --- 1. Stop all running pods and the machine (needs the binary) ---

REAL_USER=$(stat -f '%Su' /dev/console)
if [ -n "$REAL_USER" ] && [ "$REAL_USER" != "root" ]; then
    echo "Stopping all pods..."
    sudo -u "$REAL_USER" /opt/tainer/bin/tainer pod rm -f -a 2>/dev/null || :

    echo "Stopping and removing the machine..."
    sudo -u "$REAL_USER" /opt/tainer/bin/tainer machine stop 2>/dev/null || :
    sudo -u "$REAL_USER" /opt/tainer/bin/tainer machine rm -f 2>/dev/null || :
fi

# --- 2. Uninstall mac-helper ---

/opt/tainer/bin/tainer-mac-helper uninstall 2>/dev/null || :

# --- 3. Restore pf firewall ---

PF_CONF="/etc/pf.conf"
PF_ANCHOR="/etc/pf.anchors/tainer"
PF_BACKUP="${PF_CONF}.tainer-backup"

if [ -f "$PF_BACKUP" ]; then
    echo "Restoring pf.conf from backup..."
    cp "$PF_BACKUP" "$PF_CONF"
    rm -f "$PF_BACKUP"
    pfctl -f "$PF_CONF" 2>/dev/null || :
elif [ -f "$PF_CONF" ]; then
    # No backup — remove our lines manually
    sed -i '' '/rdr-anchor "tainer"/d' "$PF_CONF"
    sed -i '' '/load anchor "tainer"/d' "$PF_CONF"
    pfctl -f "$PF_CONF" 2>/dev/null || :
fi
rm -f "$PF_ANCHOR"

# --- 4. Remove DNS resolver ---

rm -f /etc/resolver/tainer.me

# --- 5. Remove PATH entry ---

rm -f /etc/paths.d/00-tainer

# --- 6. Remove man page config ---

rm -f /usr/local/etc/man.d/tainer.man.conf

# --- 7. Remove krunkit compatibility symlink ---

if [ -L /opt/podman/lib ]; then
    rm -f /opt/podman/lib
    rmdir /opt/podman 2>/dev/null || :
fi

# --- 8. Remove user config and container state ---

if [ -n "$REAL_USER" ] && [ "$REAL_USER" != "root" ]; then
    REAL_HOME=$(dscl . -read "/Users/$REAL_USER" NFSHomeDirectory | awk '{print $2}')
    if [ -d "$REAL_HOME/.config/tainer" ]; then
        echo "Removing user config at $REAL_HOME/.config/tainer..."
        rm -rf "$REAL_HOME/.config/tainer"
    fi
    # Remove tainer machine state (leaves podman machines intact)
    rm -rf "$REAL_HOME/.local/share/containers/podman/machine/tainer-machine-default" 2>/dev/null || :
fi

# --- 9. Remove /opt/tainer ---

echo "Removing /opt/tainer..."
rm -rf /opt/tainer

# --- 10. Forget the package receipt ---

pkgutil --forget io.cyber5.tainer >/dev/null 2>&1 || :
echo "Package receipt removed."

echo "Tainer has been uninstalled."
