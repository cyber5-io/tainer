#!/bin/sh
set -e

groupadd -f cyberstack
systemctl daemon-reload
systemctl enable --now cyberstack.socket

cat <<'MSG'

tainer installed. To use it without sudo, add yourself to the cyberstack group:

    sudo usermod -aG cyberstack $USER

then log out and back in (or run: newgrp cyberstack).
MSG

exit 0
