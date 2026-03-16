#!/bin/sh
set -e
cd /var/www/html

# WP-CLI wrapper: runs as tainer user with sufficient memory
wp() { su-exec tainer php -d memory_limit=512M /usr/local/bin/wp "$@"; }

# Fix ownership on mount-point directories created by Podman as root
chown tainer /var/www/html
[ -d wp-content ] && chown -R tainer wp-content

# Download WordPress if not present
if [ ! -f wp-load.php ]; then
    if [ -z "$(ls -A wp-content/themes 2>/dev/null)" ]; then
        # First install — full download, defaults land in data mounts
        wp core download
    else
        # Version swap — core only, preserve existing themes/plugins
        wp core download --skip-content
    fi
fi

# Generate wp-config.php if empty or missing (writes to data/ via bind mount)
# Use -s (non-empty) because the wizard creates an empty placeholder file
if [ ! -s wp-config.php ]; then
    wp config create \
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
