#!/bin/sh
set -e
cd /var/www/html

# WP-CLI wrapper: runs as tainer user with sufficient memory
wp() { su-exec tainer php -d memory_limit=512M /usr/local/bin/wp "$@"; }

# Fix ownership
chown tainer /var/www/html
chown -R tainer /var/www/data

# Download WordPress if not present
if [ ! -f wp-load.php ]; then
    wp core download
fi

# Create symlinks from app to data (idempotent)
# wp-config.php
if [ ! -L wp-config.php ]; then
    rm -f wp-config.php
    ln -s ../data/wp-config.php wp-config.php
fi

# wp-content subdirs: move defaults to data/ then symlink
mkdir -p wp-content
for dir in plugins themes uploads; do
    if [ ! -L "wp-content/$dir" ]; then
        # If data dir is empty and app dir has content, move defaults over
        if [ -d "wp-content/$dir" ] && [ -z "$(ls -A /var/www/data/wp-content/$dir 2>/dev/null)" ]; then
            cp -a "wp-content/$dir/." "/var/www/data/wp-content/$dir/" 2>/dev/null || true
        fi
        rm -rf "wp-content/$dir"
        ln -s "../../data/wp-content/$dir" "wp-content/$dir"
    fi
done

# Generate wp-config.php in data if empty or missing
if [ ! -s /var/www/data/wp-config.php ]; then
    wp config create --force --skip-check \
        --dbname="$DB_NAME" --dbuser="$DB_USER" \
        --dbpass="$DB_PASSWORD" --dbhost="$DB_HOST"
    wp config set FS_METHOD direct
fi

# Wait for database
db_ready=0
for i in $(seq 1 60); do
    if wp db check >/dev/null 2>&1; then
        db_ready=1
        break
    fi
    sleep 1
done
if [ "$db_ready" -ne 1 ]; then
    echo "ERROR: database not reachable after 60s" >&2
    exit 1
fi

# Install WordPress if DB is empty
if ! wp core is-installed 2>/dev/null; then
    wp core install \
        --url="$WP_HOME" --title="Tainer Site" \
        --admin_user=tainer --admin_password=tainer \
        --admin_email=tainer@tainer.me
fi

# WP-CLI config so wp commands work from any directory
mkdir -p /home/tainer/.wp-cli
cat > /home/tainer/.wp-cli/config.yml << 'WPCLIEOF'
path: /var/www/html
WPCLIEOF
chown -R tainer /home/tainer/.wp-cli

echo "WordPress ready"
