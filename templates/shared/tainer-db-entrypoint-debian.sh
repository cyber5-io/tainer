#!/bin/sh
# Creates the tainer user for consistency (Debian commands), then chains to original DB entrypoint.

GROUP_NAME=$(awk -F: -v gid="$TAINER_GID" '$3 == gid {print $1}' /etc/group)
if [ -z "$GROUP_NAME" ]; then
    groupadd -g "$TAINER_GID" tainer
    GROUP_NAME=tainer
fi

if ! id tainer >/dev/null 2>&1; then
    useradd -u "$TAINER_UID" -g "$GROUP_NAME" -m -d /home/tainer tainer
fi

exec docker-entrypoint.sh "$@"
