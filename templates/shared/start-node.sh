#!/bin/sh
# Starts SSHD (as root for port 22) then keeps container alive.
# Node.js apps are started manually by the developer or via post-deploy.
/usr/sbin/sshd
exec su-exec tainer tail -f /dev/null
