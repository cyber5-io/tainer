#!/bin/sh
# Configures PHP-FPM to run as tainer user, then starts FPM.
GROUP_NAME=$(id -gn tainer)
sed -i "s/^user = .*/user = tainer/" /usr/local/etc/php-fpm.d/www.conf
sed -i "s/^group = .*/group = $GROUP_NAME/" /usr/local/etc/php-fpm.d/www.conf

# Generate PHP limits from env vars (set by tainer from tainer.yaml)
cat > /usr/local/etc/php/conf.d/tainer-limits.ini <<EOF
upload_max_filesize=${PHP_UPLOAD_MAX_FILESIZE:-2G}
post_max_size=${PHP_POST_MAX_SIZE:-2G}
memory_limit=${PHP_MEMORY_LIMIT:-512M}
max_execution_time=${PHP_MAX_EXECUTION_TIME:-300}
max_input_vars=${PHP_MAX_INPUT_VARS:-10000}
EOF

exec php-fpm --nodaemonize
