#!/bin/sh
set -e
cd /var/www/html

# WP-CLI wrapper: runs as tainer user with sufficient memory
wp() { su-exec tainer php -d memory_limit=512M /usr/local/bin/wp "$@"; }

# Fix ownership on app dir
chown tainer /var/www/html

# Download WordPress if not present
if [ ! -f wp-load.php ]; then
    wp core download
fi

# Symlink persistent data dirs from app to /data/ mounts
# Remove WP-shipped dirs/files and replace with symlinks to data mounts
for dir in wp-content/uploads wp-content/plugins wp-content/themes; do
    if [ -d "/data/$dir" ]; then
        rm -rf "/var/www/html/$dir"
        ln -sf "/data/$dir" "/var/www/html/$dir"
        chown -h tainer "/var/www/html/$dir"
    fi
done

# Symlink wp-config.php to data mount
if [ -e /data/wp-config.php ]; then
    rm -f /var/www/html/wp-config.php
    ln -sf /data/wp-config.php /var/www/html/wp-config.php
    chown -h tainer /var/www/html/wp-config.php
fi

# Generate wp-config.php if empty or missing
if [ ! -s /data/wp-config.php ]; then
    wp config create --force \
        --dbname="$DB_NAME" --dbuser="$DB_USER" \
        --dbpass="$DB_PASSWORD" --dbhost="$DB_HOST"
    wp config set FS_METHOD direct
fi

# Install WordPress if DB is empty
if ! wp core is-installed 2>/dev/null; then
    # Wait for database
    db_ready=0
    for i in $(seq 1 30); do
        if wp db check 2>/dev/null; then
            db_ready=1
            break
        fi
        sleep 1
    done
    if [ "$db_ready" -ne 1 ]; then
        echo "ERROR: database not reachable after 30s" >&2
        exit 1
    fi
    wp core install \
        --url="$WP_HOME" --title="Tainer Site" \
        --admin_user=admin --admin_password=admin \
        --admin_email=admin@example.com
fi

echo "WordPress ready"
