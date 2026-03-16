#!/bin/sh
set -e
cd /app

# Create a starter Next.js app if app/ is empty
if [ ! -f package.json ]; then
    echo "Creating starter Next.js app..."

    cat > package.json << 'PKGEOF'
{
  "name": "tainer-app",
  "version": "1.0.0",
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next dev -p 3000"
  },
  "dependencies": {
    "next": "^15",
    "react": "^19",
    "react-dom": "^19"
  }
}
PKGEOF

    cat > next.config.mjs << 'CFGEOF'
/** @type {import('next').NextConfig} */
const nextConfig = {};
export default nextConfig;
CFGEOF

    mkdir -p app

    cat > app/layout.tsx << 'LAYOUTEOF'
export const metadata = { title: 'Tainer - Next.js' };
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return <html><body>{children}</body></html>;
}
LAYOUTEOF

    cat > app/page.tsx << 'PAGEEOF'
export default function Home() {
  return (
    <div style={{ fontFamily: 'system-ui', maxWidth: 600, margin: '80px auto', padding: '0 20px' }}>
      <h1>Tainer - Next.js</h1>
      <p>Your Next.js project is ready. Edit <code>app/app/page.tsx</code> to get started.</p>
    </div>
  );
}
PAGEEOF

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
        for pkg in next react react-dom; do
            [ -d "$GLOBAL_DIR/$pkg" ] && su-exec tainer ln -s "$GLOBAL_DIR/$pkg" node_modules/$pkg
        done
    fi
fi

touch /tmp/.tainer-ready
echo "Next.js ready"
