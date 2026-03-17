#!/bin/sh
# Starts SSHD then the app on port 3000.
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
    exec su-exec tainer yarn start
fi

# Fallback: serve a welcome page if post-deploy never ran
mkdir -p /tmp/tainer-welcome
cat > /tmp/tainer-welcome/index.html << 'EOF'
<!DOCTYPE html>
<html><head><title>Tainer</title></head>
<body style="font-family:system-ui;max-width:600px;margin:80px auto;padding:0 20px">
<h1>Tainer</h1>
<p>Your project is starting up. If you see this page, post-deploy may still be running.</p>
</body></html>
EOF
exec su-exec tainer node -e "
const http = require('http');
const fs = require('fs');
const html = fs.readFileSync('/tmp/tainer-welcome/index.html');
http.createServer((req, res) => {
    res.writeHead(200, {'Content-Type': 'text/html'});
    res.end(html);
}).listen(3000);
"
