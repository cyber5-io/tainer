#!/bin/sh
set -e
cd /app
if [ -f package.json ]; then
    npm install
fi
echo "Node.js project setup complete"
