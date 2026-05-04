#!/bin/sh
# Starts SSHD then the app on port 3000.
# If the app crashes, the container stays alive for SSH access.
/usr/sbin/sshd

# Wait for post-deploy to finish (writes marker when done)
waited=0
while [ ! -f /tmp/.tainer-ready ] && [ "$waited" -lt 300 ]; do
    sleep 2
    waited=$((waited + 2))
done

# If app has been set up by post-deploy, start it
if [ -f /var/www/html/package.json ]; then
    cd /var/www/html

    # Install dependencies if missing
    if [ ! -d node_modules ]; then
        echo "Installing dependencies..."
        su-exec tainer yarn install || echo "WARNING: yarn install failed"
    fi

    su-exec tainer yarn start || echo "WARNING: app exited with code $?. Container is still running — use SSH or tainer exec to debug."
fi

# Keep the container alive regardless of whether the app started, crashed, or
# was never set up. SSHD is running so users can always SSH in.
echo "Container is running. SSH is available."
tail -f /dev/null
