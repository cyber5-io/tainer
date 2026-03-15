#!/bin/sh
# Runs on first start only inside the caddy container

set -e

cd /var/www/html

# WP-CLI wrapper with sufficient memory
wp() { php -d memory_limit=512M /usr/local/bin/wp "$@"; }

# Download WordPress if not present
if [ ! -f wp-config.php ] && [ ! -f wp-load.php ]; then
    wp core download --allow-root
fi

# Create wp-config.php if not present
if [ ! -f wp-config.php ]; then
    wp config create \
        --dbname="$DB_NAME" \
        --dbuser="$DB_USER" \
        --dbpass="$DB_PASSWORD" \
        --dbhost="$DB_HOST" \
        --allow-root

    # Wait for database
    for i in $(seq 1 30); do
        if wp db check --allow-root 2>/dev/null; then
            break
        fi
        sleep 1
    done

    wp core install \
        --url="$WP_HOME" \
        --title="Tainer Site" \
        --admin_user=admin \
        --admin_password=admin \
        --admin_email=admin@example.com \
        --allow-root
fi

echo "WordPress setup complete"
