#!/bin/sh
# Starts SSHD (as root for port 22) then Caddy (as tainer user).
/usr/sbin/sshd
exec su-exec tainer caddy run --config /etc/caddy/Caddyfile
