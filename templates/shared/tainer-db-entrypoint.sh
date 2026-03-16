#!/bin/sh
# Creates the tainer user for consistency, then chains to the original DB entrypoint.
addgroup -g "$TAINER_GID" tainer 2>/dev/null
adduser -u "$TAINER_UID" -G tainer -D -h /home/tainer tainer 2>/dev/null
exec docker-entrypoint.sh "$@"
