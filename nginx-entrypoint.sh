# Он будет создавать минимальные options-ssl-nginx.conf, ssl-dhparams.pem и самоподписанный сертификат, если их ещё нет
# чтобы nginx корректно мог запуститься, а потом сгенерировать валидные сертификаты через certbot

#!/bin/sh
set -e

# Каталог сертификатов
CERT_DIR="/etc/letsencrypt/live/xn----9sbda5aajj0ab4c.xn--p1ai"

# Создаём папку для сертификатов, если отсутствует
mkdir -p "$CERT_DIR"

# Самоподписанный сертификат (если нет настоящего)
if [ ! -f "$CERT_DIR/fullchain.pem" ]; then
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$CERT_DIR/privkey.pem" \
        -out "$CERT_DIR/fullchain.pem" \
        -subj "/CN=xn----9sbda5aajj0ab4c.xn--p1ai"
fi

# Базовый файл options-ssl-nginx.conf (если отсутствует)
if [ ! -f /etc/letsencrypt/options-ssl-nginx.conf ]; then
    cat > /etc/letsencrypt/options-ssl-nginx.conf <<'EOF'
ssl_session_cache shared:le_nginx_SSL:10m;
ssl_session_timeout 1440m;
ssl_session_tickets off;
ssl_protocols TLSv1.2 TLSv1.3;
ssl_prefer_server_ciphers off;
ssl_ciphers "ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384";
EOF
fi

# ssl-dhparams.pem (если нет – используем готовый, чтобы не тормозить запуск)
if [ ! -f /etc/letsencrypt/ssl-dhparams.pem ]; then
    # Готовый dhparams из образа certbot (скопируем заранее) или сгенерируем короткий
    # Вариант 1: сгенерировать 2048 (медленно)
    # openssl dhparam -out /etc/letsencrypt/ssl-dhparams.pem 2048
    # Вариант 2 (быстрый): использовать dhparams по умолчанию из alpine
    cp /etc/ssl/dhparam.pem /etc/letsencrypt/ssl-dhparams.pem 2>/dev/null || \
    openssl dhparam -out /etc/letsencrypt/ssl-dhparams.pem 1024
fi