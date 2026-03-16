#!/bin/sh
set -e
cd /app

# Clone Kompozi repo if not already present (idempotent)
if [ ! -f package.json ]; then
    echo "Cloning Kompozi..."
    tmp=$(mktemp -d)
    chmod 777 "$tmp"
    su-exec tainer git clone --depth 1 https://github.com/cyber5-io/kompozi.git "$tmp/kompozi"
    cp -a "$tmp/kompozi/." /app/
    rm -rf /app/.git "$tmp"
    chown -R tainer /app

    # Override start script for local dev mode
    su-exec tainer sed -i 's/"start": "next start"/"start": "next dev"/' /app/package.json
fi

# Install dependencies
if [ -f package.json ] && [ ! -d node_modules ]; then
    echo "Installing dependencies..."
    su-exec tainer yarn install
fi

# Wait for database
echo "Waiting for database..."
db_ready=0
for i in $(seq 1 60); do
    if pg_isready -h 127.0.0.1 -p 5432 -q 2>/dev/null; then
        db_ready=1
        break
    fi
    sleep 1
done
if [ "$db_ready" -ne 1 ]; then
    echo "ERROR: database not reachable after 60s" >&2
    exit 1
fi

# In dev mode, PayloadCMS push:true creates tables on first request.
# Start dev server temporarily to trigger schema push and seed admin.
ADMIN_EXISTS=$(PGPASSWORD="$DB_PASSWORD" psql -h 127.0.0.1 -p 5432 -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT COUNT(*) FROM users" 2>/dev/null || echo "0")

if [ "$ADMIN_EXISTS" = "0" ]; then
    echo "Starting dev server to initialize database..."
    su-exec tainer yarn dev &
    APP_PID=$!

    # Wait for app to be ready (dev mode compiles on first request)
    app_ready=0
    for i in $(seq 1 90); do
        if curl -sf http://127.0.0.1:3000/api/users/me >/dev/null 2>&1; then
            app_ready=1
            break
        fi
        # Also accept error responses (tables being created)
        status=$(curl -so /dev/null -w '%{http_code}' http://127.0.0.1:3000/api/users/me 2>/dev/null || echo "000")
        if [ "$status" != "000" ]; then
            # Server is responding, give it a moment for schema push
            sleep 5
            app_ready=1
            break
        fi
        sleep 2
    done

    if [ "$app_ready" -eq 1 ]; then
        # Wait a bit more for schema push to complete
        sleep 3
        curl -sf -X POST http://127.0.0.1:3000/api/users/first-register \
            -H "Content-Type: application/json" \
            -d "{\"email\":\"${KOMPOZI_ADMIN_EMAIL}\",\"password\":\"${KOMPOZI_ADMIN_PASSWORD}\"}" \
            >/dev/null 2>&1 && echo "Admin user created" || echo "Warning: could not create admin user"
    else
        echo "Warning: app did not start within 180s, skipping admin seed"
    fi

    kill "$APP_PID" 2>/dev/null || true
    wait "$APP_PID" 2>/dev/null || true
fi

touch /tmp/.tainer-ready
echo "Kompozi ready"
