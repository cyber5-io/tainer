#!/bin/sh
# Creates the tainer user for consistency (Debian commands), then chains to original DB entrypoint.
groupadd -g "$TAINER_GID" tainer 2>/dev/null || true
useradd -u "$TAINER_UID" -g "$TAINER_GID" -m -d /home/tainer tainer 2>/dev/null || true
exec docker-entrypoint.sh "$@"
