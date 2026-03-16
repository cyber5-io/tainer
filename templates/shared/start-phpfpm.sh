#!/bin/sh
# Configures PHP-FPM to run as tainer user, then starts FPM.
GROUP_NAME=$(id -gn tainer)
sed -i "s/^user = .*/user = tainer/" /usr/local/etc/php-fpm.d/www.conf
sed -i "s/^group = .*/group = $GROUP_NAME/" /usr/local/etc/php-fpm.d/www.conf
exec php-fpm --nodaemonize
