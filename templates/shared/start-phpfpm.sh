#!/bin/sh
# Configures PHP-FPM to run as tainer user, then starts FPM.
sed -i "s/^user = .*/user = tainer/" /usr/local/etc/php-fpm.d/www.conf
sed -i "s/^group = .*/group = tainer/" /usr/local/etc/php-fpm.d/www.conf
exec php-fpm --nodaemonize
