#!/bin/sh
set -e
cd /var/www/html

# WP-CLI wrapper: runs as tainer user with sufficient memory
wp() { su-exec tainer php -d memory_limit=512M /usr/local/bin/wp "$@"; }

# Fix ownership on app dir and wp-content mount
chown tainer /var/www/html
[ -d wp-content ] && chown tainer wp-content

# Download WordPress if not present
if [ ! -f wp-load.php ]; then
    wp core download
fi

# Generate wp-config.php if empty or missing
if [ ! -s wp-config.php ]; then
    wp config create --force \
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
        --admin_user=admin --admin_password=admin \
        --admin_email=admin@example.com
fi

echo "WordPress ready"
