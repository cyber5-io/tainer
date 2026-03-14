#!/bin/sh
set -e
cd /var/www/html
if [ -f composer.json ]; then
    composer install --no-interaction
fi
echo "PHP project setup complete"
