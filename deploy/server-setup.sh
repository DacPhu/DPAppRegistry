#!/usr/bin/env bash
# Prepares an Ubuntu or Debian server for DPAppRegistry: Docker, the stack's
# settings and secrets, and the nginx sites. deploy.sh runs it as root with the
# domain, STORE and ADMIN as its arguments, after copying compose.yaml and
# nginx/ to /tmp/dpappregistry-setup. Safe to run again: it installs only what
# is missing and never replaces secrets it made before.

set -euo pipefail

DOMAIN=$1
# on: the store page at /, the dashboard at /registry/ (nginx/store.conf).
# off: the dashboard at / (nginx/registry.conf).
STORE=${2:-on}
# tunnel: the dashboard only on the admin site, 127.0.0.1:3310 (nginx/admin.conf),
# which admins reach through an SSH tunnel; the public site answers 404 in its
# place and for signing in and up. public: the dashboard on the public site.
ADMIN=${3:-tunnel}
case "$ADMIN" in tunnel | public) ;; *) echo "ADMIN is tunnel or public, not $ADMIN." >&2; exit 1 ;; esac
APP_DIR=/opt/dpappregistry
WEB_DIR=/var/www/dpappregistry
STAGE=/tmp/dpappregistry-setup
CERT=/etc/letsencrypt/live/$DOMAIN/fullchain.pem

missing=()
for pkg in docker.io docker-compose-v2 docker-buildx nginx certbot rsync curl logrotate iproute2; do
  dpkg -s "$pkg" >/dev/null 2>&1 || missing+=("$pkg")
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "Installing ${missing[*]}"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq "${missing[@]}" >/dev/null
fi
systemctl enable --now docker >/dev/null 2>&1

# The admin site's port must be free or nginx's, checked before anything is
# changed: nginx -t doesn't try to bind it, and a reload that can't bind it
# reports success while nginx goes on with its old configuration.
if [ "$ADMIN" = tunnel ]; then
  others=$(ss -Hltnp 'sport = :3310' | grep -v '"nginx"' || true)
  if [ -n "$others" ]; then
    echo "Port 3310, which the admin site needs, is taken by something other than nginx; nothing was changed:" >&2
    echo "$others" >&2
    exit 1
  fi
fi

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
# Where compose.yaml publishes the API's metrics: on the IPv4 gateway of the
# infra repo's Docker network, where its Prometheus scrapes them while the API
# stays off that network; on the loopback when there is none. Found again every
# run, as the network can be recreated with another address.
metrics_ip=
for gateway in $(docker network inspect infra --format '{{range .IPAM.Config}}{{.Gateway}} {{end}}' 2>/dev/null || true); do
  if [[ $gateway =~ ^[0-9]+(\.[0-9]+){3}$ ]]; then
    metrics_ip=$gateway
    break
  fi
done
if [ -z "$metrics_ip" ]; then
  echo "WARNING: no Docker network \"infra\" with an IPv4 gateway. The API's metrics go to 127.0.0.1:9326, where Prometheus can't scrape them; once the infra repo's network exists, run this again." >&2
  metrics_ip=127.0.0.1
fi
sed -i '/^METRICS_PUBLISH=/d' "$APP_DIR/.env"
echo "METRICS_PUBLISH=$metrics_ip:9326" >>"$APP_DIR/.env"
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
# around them is root's only. Prometheus's requests for /metrics arrive from a
# Docker network, its own or, through Docker's NAT, this stack's gateway:
# METRICS_ALLOW admits both and refuses any public address. LOG_DIR is the
# app_logs volume, which the infra repo's Alloy reads.
cat >"$APP_DIR/backend.env" <<EOF
PORT=9000
API_URL=https://$DOMAIN/api
ALLOWED_CORS=https://$DOMAIN
TRUSTED_PROXIES=172.30.0.1
GIN_MODE=release
METRICS_ADDR=:9326
METRICS_ALLOW=172.16.0.0/12,127.0.0.0/8
LOG_DIR=/app/logs
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

# The public site's access log, one JSON object per request for the infra
# repo's Alloy, which reads /var/log/nginx/sites. Every night logrotate renames
# it, keeping 3, and nginx reopens its file at once; the renamed one is
# compressed only the night after, so the shipper can finish reading it.
install -d -m 755 /var/log/nginx/sites
cat >/etc/logrotate.d/dpappregistry-nginx <<'EOF'
/var/log/nginx/sites/dpappregistry.access.log {
    daily
    rotate 3
    compress
    delaycompress
    missingok
    notifempty
    create 0644 www-data adm
    sharedscripts
    postrotate
        nginx -s reopen
    endscript
}
EOF

# nginx also serves the server's other sites (mitekbro.com). Every nginx file
# changed below is copied first. If a step fails, nginx refuses the result, or
# the admin site doesn't answer after the reload, the copies go back, and nginx
# is reloaded with them if it had been already: a file left behind that nginx
# can't run would only show at its next restart, and stop every site.
snippets=/etc/nginx/snippets
site=/etc/nginx/sites-available/dpappregistry
admin_site=/etc/nginx/sites-available/dpappregistry-admin
nginx_files=(
  /etc/nginx/conf.d/dpappregistry-log-format.conf
  "$snippets/dpappregistry-api.conf" "$snippets/dpappregistry-files.conf"
  "$snippets/dpappregistry-dashboard.conf" "$snippets/dpappregistry-dashboard-hidden.conf"
  "$site" /etc/nginx/sites-enabled/dpappregistry
  "$admin_site" /etc/nginx/sites-enabled/dpappregistry-admin
)
saved=$(mktemp -d)
for file in "${nginx_files[@]}"; do
  if [ -e "$file" ] || [ -L "$file" ]; then
    mkdir -p "$saved$(dirname "$file")"
    cp -a "$file" "$saved$file"
  fi
done
reloaded=
restore_nginx() {
  local file
  for file in "${nginx_files[@]}"; do
    rm -f "$file"
    if [ -e "$saved$file" ] || [ -L "$saved$file" ]; then
      cp -a "$saved$file" "$file"
    fi
  done
  rm -rf "$saved"
  if [ -z "$reloaded" ]; then
    echo "nginx's previous files are back, and it was not reloaded." >&2
  elif nginx -t -q && systemctl reload nginx; then
    echo "nginx's previous files are back, and it was reloaded with them." >&2
  else
    echo "nginx's previous files are back, but reloading nginx with them failed (see above)." >&2
  fi
}
trap restore_nginx EXIT

install -m 644 "$STAGE/log-format.conf" /etc/nginx/conf.d/dpappregistry-log-format.conf
install -m 644 "$STAGE/api.conf" "$snippets/dpappregistry-api.conf"
install -m 644 "$STAGE/files.conf" "$snippets/dpappregistry-files.conf"

# The dashboard's locations (nginx/dashboard.conf) and the 404s in their place
# (nginx/dashboard-hidden.conf). The public site has the dashboard at /, or
# beside the store page at /registry/; the admin site always at /.
dashboard_base=/
hidden_base=/
if [ "$STORE" = on ]; then
  hidden_base=/registry/
  if [ "$ADMIN" = public ]; then
    dashboard_base=/registry/
  fi
fi
dashboard_root=$WEB_DIR/registry
if [ "$dashboard_base" = /registry/ ]; then
  dashboard_root=$WEB_DIR
fi
sed -e "s|{{BASE}}|$dashboard_base|g" -e "s|{{ROOT}}|$dashboard_root|g" "$STAGE/dashboard.conf" >"$snippets/dpappregistry-dashboard.conf"
sed "s|{{BASE}}|$hidden_base|g" "$STAGE/dashboard-hidden.conf" >"$snippets/dpappregistry-dashboard-hidden.conf"
chmod 644 "$snippets/dpappregistry-dashboard.conf" "$snippets/dpappregistry-dashboard-hidden.conf"

# The public site needs its certificate; until deploy.sh gets one, the domain
# is left to the server's default site, which answers the certificate check.
if [ -f "$CERT" ]; then
  page=store.conf
  if [ "$STORE" = off ]; then
    page=registry.conf
  fi
  dashboard=dpappregistry-dashboard.conf
  if [ "$ADMIN" = tunnel ]; then
    dashboard=dpappregistry-dashboard-hidden.conf
  fi
  sed -e "s/{{DOMAIN}}/$DOMAIN/g" -e "s/{{DASHBOARD}}/$dashboard/g" "$STAGE/$page" >"$site"
  ln -sfn "$site" /etc/nginx/sites-enabled/dpappregistry
else
  rm -f /etc/nginx/sites-enabled/dpappregistry
fi

# The admin site, for the SSH tunnel.
if [ "$ADMIN" = tunnel ]; then
  install -m 644 "$STAGE/admin.conf" "$admin_site"
  ln -sfn "$admin_site" /etc/nginx/sites-enabled/dpappregistry-admin
else
  rm -f /etc/nginx/sites-enabled/dpappregistry-admin "$admin_site"
fi

nginx -t -q
reloaded=1
systemctl reload nginx

# The admin site must answer on the server's loopback, where the SSH tunnel
# ends, from nginx itself: a reload that can't bind the port goes on with the
# old configuration all the same. nginx takes a moment to apply a reload.
# Before deploy.sh has built the dashboard (on a new server), any answer from
# nginx will do.
if [ "$ADMIN" = tunnel ]; then
  fail=(-f)
  if [ ! -f "$WEB_DIR/registry/index.html" ]; then
    fail=()
  fi
  for attempt in $(seq 10); do
    listeners=$(ss -Hltnp 'sport = :3310')
    if [[ $listeners == *'"nginx"'* ]] && curl "${fail[@]}" -sS --max-time 5 -o /dev/null http://127.0.0.1:3310/; then
      break
    fi
    if [ "$attempt" = 10 ]; then
      echo "The admin site doesn't answer at http://127.0.0.1:3310/ from nginx." >&2
      exit 1
    fi
    sleep 1
  done
fi
trap - EXIT
rm -rf "$saved" "$STAGE"
