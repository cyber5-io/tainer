#!/bin/sh
# Creates the tainer user with uid/gid from environment variables.
# Every Tainer container uses this entrypoint.

# Reuse existing group if GID is taken, otherwise create 'tainer' group
GROUP_NAME=$(awk -F: -v gid="$TAINER_GID" '$3 == gid {print $1}' /etc/group)
if [ -z "$GROUP_NAME" ]; then
    addgroup -g "$TAINER_GID" tainer
    GROUP_NAME=tainer
fi

# Create tainer user with the resolved group
if ! id tainer >/dev/null 2>&1; then
    adduser -u "$TAINER_UID" -G "$GROUP_NAME" -D -h /home/tainer tainer
    echo "tainer:tainer" | chpasswd 2>/dev/null || true
fi

mkdir -p /home/tainer/.ssh

# Symlink app directory from home for easy navigation
if [ -d /var/www/html ] && [ ! -e /home/tainer/www ]; then
    ln -s /var/www/html /home/tainer/www
fi
if [ -d /app ] && [ ! -e /home/tainer/app ]; then
    ln -s /app /home/tainer/app
fi

# MySQL/MariaDB client config for passwordless 'mysql' command
if [ -n "$DB_HOST" ] && [ -n "$DB_PASSWORD" ] && command -v mysql >/dev/null 2>&1; then
    cat > /home/tainer/.my.cnf << MYCNF
[client]
host=$DB_HOST
port=${DB_PORT:-3306}
user=${DB_USER:-tainer}
password=$DB_PASSWORD
database=${DB_NAME:-tainer}
MYCNF
    chmod 600 /home/tainer/.my.cnf
fi

# PostgreSQL client config for passwordless 'psql' command
if [ -n "$DB_HOST" ] && [ -n "$DB_PASSWORD" ] && command -v psql >/dev/null 2>&1; then
    printf '%s:%s:%s:%s:%s\n' \
        "${DB_HOST}" "${DB_PORT:-5432}" "${DB_NAME:-tainer}" "${DB_USER:-tainer}" "${DB_PASSWORD}" \
        > /home/tainer/.pgpass
    chmod 600 /home/tainer/.pgpass
    cat > /home/tainer/.pg_env << PGENV
export PGHOST=$DB_HOST
export PGPORT=${DB_PORT:-5432}
export PGDATABASE=${DB_NAME:-tainer}
export PGUSER=${DB_USER:-tainer}
PGENV
    # Source PG env vars in profile so psql works without arguments
    if ! grep -q '.pg_env' /home/tainer/.profile 2>/dev/null; then
        echo '[ -f ~/.pg_env ] && . ~/.pg_env' >> /home/tainer/.profile
    fi
fi

chown -R tainer:"$GROUP_NAME" /home/tainer
exec "$@"
