#!/bin/sh
# Creates the tainer user with uid/gid from environment variables.
# Every Tainer container uses this entrypoint.
addgroup -g "$TAINER_GID" tainer 2>/dev/null
adduser -u "$TAINER_UID" -G tainer -D -h /home/tainer tainer 2>/dev/null
mkdir -p /home/tainer/.ssh
chown -R tainer:tainer /home/tainer
exec "$@"
