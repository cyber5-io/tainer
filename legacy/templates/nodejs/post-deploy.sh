#!/bin/sh
set -e
cd /var/www/html

# Create a starter Node.js app if html/ is empty
if [ ! -f package.json ]; then
    echo "Creating starter Node.js app..."

    cat > package.json << 'PKGEOF'
{
  "name": "tainer-app",
  "version": "1.0.0",
  "scripts": {
    "start": "nodemon --legacy-watch index.js"
  }
}
PKGEOF

    cat > index.js << 'JSEOF'
const http = require('http');

const server = http.createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/html' });
  res.end(`<!DOCTYPE html>
<html><head><title>Tainer - Node.js</title></head>
<body style="font-family:system-ui;max-width:600px;margin:80px auto;padding:0 20px">
<h1>Tainer - Node.js</h1>
<p>Your Node.js project is ready. Edit <code>html/index.js</code> to get started.</p>
<p>Node ${process.version}</p>
</body></html>`);
});

server.listen(3000, () => {
  console.log('Server running on port 3000');
});
JSEOF

    chown -R tainer /var/www/html
fi

# Install dependencies if needed
if [ -f package.json ] && [ ! -d node_modules ] && [ -f yarn.lock ]; then
    su-exec tainer yarn install
fi

touch /tmp/.tainer-ready
echo "Node.js ready"
