#!/bin/sh
set -e
cd /app

# Create a starter Nuxt app if app/ is empty
if [ ! -f package.json ]; then
    echo "Creating starter Nuxt app..."

    cat > package.json << 'PKGEOF'
{
  "name": "tainer-app",
  "version": "1.0.0",
  "scripts": {
    "dev": "nuxt dev",
    "build": "nuxt build",
    "start": "nuxt dev --port 3000 --host 0.0.0.0"
  },
  "dependencies": {
    "nuxt": "^3"
  }
}
PKGEOF

    cat > nuxt.config.ts << 'CFGEOF'
export default defineNuxtConfig({
  devtools: { enabled: false },
  devServer: { port: 3000, host: '0.0.0.0' },
  nitro: { port: 3000 },
});
CFGEOF

    cat > app.vue << 'VUEEOF'
<template>
  <div style="font-family: system-ui; max-width: 600px; margin: 80px auto; padding: 0 20px">
    <h1>Tainer - Nuxt.js</h1>
    <p>Your Nuxt project is ready. Edit <code>app/app.vue</code> to get started.</p>
  </div>
</template>
VUEEOF

    chown -R tainer /app
fi

# Link to globally pre-installed packages or install from yarn.lock
if [ -f package.json ] && [ ! -d node_modules ]; then
    if [ -f yarn.lock ]; then
        echo "Installing dependencies (this may take a minute)..."
        su-exec tainer yarn install
    else
        echo "Linking pre-installed dependencies..."
        su-exec tainer mkdir -p node_modules
        GLOBAL_DIR=$(yarn global dir)/node_modules
        for pkg in nuxt; do
            [ -d "$GLOBAL_DIR/$pkg" ] && su-exec tainer ln -s "$GLOBAL_DIR/$pkg" node_modules/$pkg
        done
    fi
fi

touch /tmp/.tainer-ready
echo "Nuxt.js ready"
