#!/bin/sh
set -e
cd /var/www/html

# Create default index.php if app/ is empty
if [ ! -f index.php ] && [ ! -f index.html ]; then
    cat > index.php << 'PHPEOF'
<?php
echo "<h1>Tainer</h1><p>Your PHP project is ready. Edit <code>app/index.php</code> to get started.</p>";
echo "<p>PHP " . PHP_VERSION . "</p>";
phpinfo();
PHPEOF
    chown tainer index.php
fi

if [ -f composer.json ]; then
    su-exec tainer composer install --no-interaction
fi
echo "PHP project ready"
