#!/bin/sh
set -e
cd /app

# Install dependencies if package.json exists
if [ -f package.json ] && [ ! -d node_modules ]; then
    su-exec tainer npm install
fi

# Run Payload migrations if available
if command -v npx >/dev/null 2>&1 && [ -f node_modules/.bin/payload ]; then
    su-exec tainer npx payload migrate 2>/dev/null || true
fi

echo "Kompozi ready"
