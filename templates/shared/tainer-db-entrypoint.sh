#!/bin/sh
# Creates the tainer user for consistency, then chains to the original DB entrypoint.

GROUP_NAME=$(awk -F: -v gid="$TAINER_GID" '$3 == gid {print $1}' /etc/group)
if [ -z "$GROUP_NAME" ]; then
    addgroup -g "$TAINER_GID" tainer
    GROUP_NAME=tainer
fi

if ! id tainer >/dev/null 2>&1; then
    adduser -u "$TAINER_UID" -G "$GROUP_NAME" -D -h /home/tainer tainer
fi

exec docker-entrypoint.sh "$@"
