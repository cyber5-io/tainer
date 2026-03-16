#!/bin/sh
set -e
cd /app

# Install dependencies if package.json exists
if [ -f package.json ] && [ ! -d node_modules ]; then
    su-exec tainer npm install
fi

echo "Nuxt.js ready"
