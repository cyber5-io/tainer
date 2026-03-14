#!/bin/sh
set -e
cd /app
if [ -f package.json ]; then
    npm install
fi
# Run Kompozi Engine database migrations
if [ -f node_modules/.bin/payload ]; then
    npx payload migrate
fi
echo "Kompozi project setup complete"
