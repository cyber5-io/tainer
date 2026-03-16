#!/bin/sh
# Creates the tainer user with uid/gid from environment variables.
# Every Tainer container uses this entrypoint.

# Reuse existing group if GID is taken, otherwise create 'tainer' group
GROUP_NAME=$(awk -F: -v gid="$TAINER_GID" '$3 == gid {print $1}' /etc/group)
if [ -z "$GROUP_NAME" ]; then
    addgroup -g "$TAINER_GID" tainer
    GROUP_NAME=tainer
fi

# Create tainer user with the resolved group
if ! id tainer >/dev/null 2>&1; then
    adduser -u "$TAINER_UID" -G "$GROUP_NAME" -D -h /home/tainer tainer
fi

mkdir -p /home/tainer/.ssh
chown -R tainer:"$GROUP_NAME" /home/tainer
exec "$@"
