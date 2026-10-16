#!/usr/bin/env bash
# Prepares an Ubuntu or Debian server for DPAppRegistry: Docker, the stack's
# settings and secrets, and the nginx site. deploy.sh runs it as root with the
# domain as its argument, after copying compose.yaml and nginx/ to
# /tmp/dpappregistry-setup. Safe to run again: it installs only what is
# missing and never replaces secrets it made before.

set -euo pipefail

DOMAIN=$1
# on: the store page at /, the dashboard at /registry/ (nginx/store.conf).
# off: the dashboard at / (nginx/registry.conf).
STORE=${2:-on}
APP_DIR=/opt/dpappregistry
WEB_DIR=/var/www/dpappregistry
STAGE=/tmp/dpappregistry-setup
CERT=/etc/letsencrypt/live/$DOMAIN/fullchain.pem

missing=()
for pkg in docker.io docker-compose-v2 docker-buildx nginx certbot rsync; do
  dpkg -s "$pkg" >/dev/null 2>&1 || missing+=("$pkg")
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "Installing ${missing[*]}"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq "${missing[@]}" >/dev/null
fi
systemctl enable --now docker >/dev/null 2>&1

mkdir -p "$APP_DIR" "$WEB_DIR/store" "$WEB_DIR/registry" /var/www/letsencrypt
chmod 700 "$APP_DIR"

# Secrets are made once and kept: the databases were created with them.
if [ ! -f "$APP_DIR/.env" ]; then
  hex() { openssl rand -hex "$1"; }
  (
    umask 077
    cat >"$APP_DIR/.env" <<EOF
STORE_DOMAIN=$DOMAIN
MONGO_ROOT_PASSWORD=$(hex 24)
REDIS_PASSWORD=$(hex 24)
GARAGE_ACCESS_KEY=GK$(hex 12)
GARAGE_SECRET_KEY=$(hex 32)
GARAGE_RPC_SECRET=$(hex 32)
GARAGE_ADMIN_TOKEN=$(hex 24)
GARAGE_METRICS_TOKEN=$(hex 24)
API_KEY=$(hex 24)
JWT_SECRET=$(hex 32)
EOF
  )
  echo "Made new secrets in $APP_DIR/.env"
fi
sed -i "s/^STORE_DOMAIN=.*/STORE_DOMAIN=$DOMAIN/" "$APP_DIR/.env"
# The key that signs the licenses the store sells, made once: keys verify
# against its public half, which release builds carry.
if ! grep -q '^LICENSE_SIGNING_KEY=' "$APP_DIR/.env"; then
  (umask 077; echo "LICENSE_SIGNING_KEY=$(openssl rand -hex 32)" >>"$APP_DIR/.env")
  echo "Made a license signing key in $APP_DIR/.env"
fi
set -a
. "$APP_DIR/.env"
set +a

# The API's settings. Readable by the container's own user; the folder
# around them is root's only.
cat >"$APP_DIR/backend.env" <<EOF
PORT=9000
API_URL=https://$DOMAIN/api
ALLOWED_CORS=https://$DOMAIN
TRUSTED_PROXIES=172.30.0.1
MONGODB_URL=mongodb://root:$MONGO_ROOT_PASSWORD@db:27017/dpappregistry?authSource=admin
API_KEY=$API_KEY
JWT_SECRET=$JWT_SECRET
PERFORMANCE_MODE=true
REDIS_HOST=cache
REDIS_PORT=6379
REDIS_PASSWORD=$REDIS_PASSWORD
REDIS_DB=0
STORAGE_DRIVER=aws
S3_ACCESS_KEY=$GARAGE_ACCESS_KEY
S3_SECRET_KEY=$GARAGE_SECRET_KEY
S3_REGION=us-east-1
S3_BUCKET_NAME=dpappregistry-public
S3_BUCKET_NAME_CDN=dpappregistry-public
S3_BUCKET_NAME_PRIVATE=dpappregistry-private
S3_ENDPOINT=https://$DOMAIN/files
S3_API_ENDPOINT=https://$DOMAIN
S3_FORCE_PATH_STYLE=true
S3_DISABLE_OBJECT_ACL=true
ENABLE_PRIVATE_APP_DOWNLOADING=false
ENABLE_TELEMETRY=false
REPORTS_ENABLED=false
TUF_ENABLED=false
STORE_OWNER=admin
STORE_URL=https://$DOMAIN
PLANS_ENABLED=${PLANS_ENABLED:-false}
PLAN_DEFAULT=${PLAN_DEFAULT:-hobby}
PLANS_CONTACT=${PLANS_CONTACT:-}
LICENSE_SIGNING_KEY=$LICENSE_SIGNING_KEY
ORDERS_ENABLED=${ORDERS_ENABLED:-false}
SEPAY_BANK_ACCOUNT=${SEPAY_BANK_ACCOUNT:-}
SEPAY_BANK_NAME=${SEPAY_BANK_NAME:-}
SEPAY_ACCOUNT_HOLDER=${SEPAY_ACCOUNT_HOLDER:-}
SEPAY_WEBHOOK_SECRET=${SEPAY_WEBHOOK_SECRET:-}
SEPAY_WEBHOOK_APIKEY=${SEPAY_WEBHOOK_APIKEY:-}
SEPAY_API_KEY=${SEPAY_API_KEY:-}
SMTP_HOST=${SMTP_HOST:-}
SMTP_PORT=${SMTP_PORT:-}
SMTP_USER=${SMTP_USER:-}
SMTP_PASS=${SMTP_PASS:-}
SMTP_FROM=${SMTP_FROM:-}
EOF
chmod 644 "$APP_DIR/backend.env"
# compose.yaml passes this to the API, so a changed backend.env restarts it.
sed -i '/^BACKEND_ENV_HASH=/d' "$APP_DIR/.env"
echo "BACKEND_ENV_HASH=$(sha256sum "$APP_DIR/backend.env" | cut -c 1-16)" >>"$APP_DIR/.env"

cat >"$APP_DIR/garage.toml" <<EOF
metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "lmdb"
metadata_auto_snapshot_interval = "6h"
replication_factor = 1
compression_level = 2

rpc_bind_addr = "[::]:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "$GARAGE_RPC_SECRET"

[s3_api]
s3_region = "us-east-1"
api_bind_addr = "[::]:3900"

[s3_web]
bind_addr = "[::]:3902"
root_domain = ".web.garage.localhost"
index = "index.html"

[admin]
api_bind_addr = "[::]:3903"
admin_token = "$GARAGE_ADMIN_TOKEN"
metrics_token = "$GARAGE_METRICS_TOKEN"
EOF
chmod 644 "$APP_DIR/garage.toml"

install -m 644 "$STAGE/compose.yaml" "$APP_DIR/compose.yaml"

# Nightly backups (backup.sh says what it keeps), at 03:15 server time.
install -m 700 "$STAGE/backup.sh" "$APP_DIR/backup.sh"
cat >/etc/systemd/system/dpappregistry-backup.service <<EOF
[Unit]
Description=Back up DPAppRegistry
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
ExecStart=$APP_DIR/backup.sh
EOF
cat >/etc/systemd/system/dpappregistry-backup.timer <<'EOF'
[Unit]
Description=Back up DPAppRegistry every night

[Timer]
OnCalendar=*-*-* 03:15:00
RandomizedDelaySec=10min
Persistent=true

[Install]
WantedBy=timers.target
EOF
systemctl daemon-reload
systemctl enable --now dpappregistry-backup.timer >/dev/null 2>&1
install -m 644 "$STAGE/api.conf" /etc/nginx/snippets/dpappregistry-api.conf
install -m 644 "$STAGE/files.conf" /etc/nginx/snippets/dpappregistry-files.conf

# The site needs its certificate; until deploy.sh gets one, the domain is
# left to the server's default site, which answers the certificate check.
if [ -f "$CERT" ]; then
  site=store.conf
  [ "$STORE" = off ] && site=registry.conf
  sed "s/{{DOMAIN}}/$DOMAIN/g" "$STAGE/$site" >/etc/nginx/sites-available/dpappregistry
  ln -sfn /etc/nginx/sites-available/dpappregistry /etc/nginx/sites-enabled/dpappregistry
else
  rm -f /etc/nginx/sites-enabled/dpappregistry
fi
rm -rf "$STAGE"

nginx -t -q
systemctl reload nginx
