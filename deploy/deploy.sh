#!/usr/bin/env bash
# Deploys DPAppRegistry to one domain on a server:
#
#   https://<domain>/api/        the API: update checks, uploads
#   https://<domain>/files/      the builds
#   http://localhost:3310        the dashboard, for admins, through an SSH tunnel
#                                to the server's 127.0.0.1:3310 (ADMIN=tunnel)
#
# With ADMIN=tunnel, the default, the public site answers 404 at / and to
# signing in and up; apps and CI, which never sign in, are unaffected. With
# ADMIN=public, the dashboard is at https://<domain>/ again. With STORE=on, the
# store page takes https://<domain>/, and an ADMIN=public dashboard moves to
# https://<domain>/registry/.
#
#   deploy/deploy.sh                  set up the server if needed, then build and start everything
#   deploy/deploy.sh web              nginx, the certificate and the pages, without rebuilding the API
#   deploy/deploy.sh store            publish only the store page (STORE=on): store/, with
#                                     deploy/store-config.js as its config.js
#   deploy/deploy.sh logs             follow the API's logs
#   deploy/deploy.sh backup           back up now (the server also does it every night)
#   deploy/deploy.sh restore-test     restore the newest backup into a scratch MongoDB and compare
#   deploy/deploy.sh account|usage …  the operator commands: plans, accounts, usage (run with no
#                                     arguments for the list)
#   deploy/deploy.sh license …        licenses sold for the store's products: issue, list, show,
#                                     revoke, pubkey (the public key DPFileSystem is built with)
#   deploy/deploy.sh order …          the store's orders: list, show, fulfil, reconcile
#
# DEPLOY_HOST, DEPLOY_USER, DEPLOY_KEY and DOMAIN change where it goes, and STORE
# and ADMIN what it serves.

set -euo pipefail

HOST="${DEPLOY_HOST:-103.110.33.182}"
LOGIN="${DEPLOY_USER:-root}@$HOST"
KEY="${DEPLOY_KEY:-$HOME/.ssh/mitekbro_deploy}"
DOMAIN="${DOMAIN:-registry.mitekbro.com}"
# on: the store page at /, the dashboard at /registry/. off: the dashboard at /.
STORE="${STORE:-off}"
case "$STORE" in on | off) ;; *) echo "STORE is on or off, not $STORE." >&2; exit 1 ;; esac
# tunnel: the dashboard only on the server's loopback (nginx/admin.conf), for
# an SSH tunnel. public: the dashboard on the domain.
ADMIN="${ADMIN:-tunnel}"
case "$ADMIN" in tunnel | public) ;; *) echo "ADMIN is tunnel or public, not $ADMIN." >&2; exit 1 ;; esac
# The upstream dashboard, pinned, since dashboard-patch.cjs edits its code: its
# :latest built on 2026-09-24. A newer one needs the patch checked against it.
DASHBOARD_IMAGE=registry.hub.docker.com/ku9nov/faynosync-dashboard@sha256:0bb14d158782769c3335aaa8ad1bcab805feb8e7dfb93d1ac09d3a2f87b636c0
APP_DIR=/opt/dpappregistry
WEB_DIR=/var/www/dpappregistry

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SSH_OPTS=(-i "$KEY" -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=20)

remote() { ssh "${SSH_OPTS[@]}" "$LOGIN" "$@"; }

setup() {
  remote 'rm -rf /tmp/dpappregistry-setup && mkdir -p /tmp/dpappregistry-setup'
  scp -q "${SSH_OPTS[@]}" "$ROOT"/deploy/nginx/*.conf "$ROOT/deploy/compose.yaml" "$ROOT/deploy/backup.sh" "$LOGIN:/tmp/dpappregistry-setup/"
  remote "bash -s -- '$DOMAIN' '$STORE' '$ADMIN'" <"$ROOT/deploy/server-setup.sh"
}

# HTTPS for the domain, once it points only at this server.
certificate() {
  if remote "test -f /etc/letsencrypt/live/$DOMAIN/fullchain.pem"; then
    return
  fi
  local ips
  ips="$(dig @1.1.1.1 +short "$DOMAIN" A | grep -E '^[0-9.]+$' | sort -u | paste -sd ' ' -)"
  if [ "$ips" != "$HOST" ]; then
    echo "$DOMAIN resolves to ${ips:-nothing}; make $HOST its only A record, then run this again." >&2
    exit 1
  fi
  # Accepts the Let's Encrypt subscriber agreement. certbot's timer renews it.
  remote certbot certonly --webroot -w /var/www/letsencrypt --cert-name "$DOMAIN" -d "$DOMAIN" \
    --non-interactive --agree-tos --register-unsafely-without-email --deploy-hook "'systemctl reload nginx'"
}

# The API, MongoDB, Redis and Garage, built from this checkout.
stack() {
  rsync -rlt --delete \
    --exclude /.git --exclude /deploy --exclude /store --exclude /tuf_keys \
    --exclude /.env.local --exclude /docker-compose/envs --exclude /docker-compose/configs \
    -e "ssh ${SSH_OPTS[*]}" "$ROOT/" "$LOGIN:$APP_DIR/src/"
  remote "cd $APP_DIR && docker compose up -d --build --remove-orphans --quiet-pull"
  remote "cd $APP_DIR && for attempt in \$(seq 40); do
      docker compose exec -T backend DPAppRegistry migrate up >/tmp/dpappregistry-migrate.log 2>&1 && exit 0
      sleep 3
    done
    cat /tmp/dpappregistry-migrate.log
    exit 1"
}

# The store page: store/, with deploy/store-config.js as its config.js. The
# first rsync leaves the live config.js alone, so the page never runs on
# store/'s generic one, and rsync swaps each changed file in whole.
store_page() {
  rsync -rlt --delete --exclude /config.js -e "ssh ${SSH_OPTS[*]}" "$ROOT/store/" "$LOGIN:$WEB_DIR/store/"
  rsync -t -e "ssh ${SSH_OPTS[*]}" "$ROOT/deploy/store-config.js" "$LOGIN:$WEB_DIR/store/config.js"
}

# The upstream dashboard, built for this API: beside the store page, under
# /registry/, or at the root without one. Through the SSH tunnel it is at the
# admin site's root and calls the API beside it, at /api; the CI upload command
# it shows still names the public API (VITE_PUBLIC_API_URL).
pages() {
  local base=/ api=https://$DOMAIN/api
  if [ "$STORE" = on ]; then
    store_page
    base=/registry/
  fi
  if [ "$ADMIN" = tunnel ]; then
    base=/
    api=/api
  fi
  rsync -rlt --delete -e "ssh ${SSH_OPTS[*]}" "$ROOT/deploy/dashboard-patch.cjs" "$ROOT/deploy/dashboard" "$LOGIN:$APP_DIR/dashboard-patch/"
  remote "rm -f $APP_DIR/dashboard-patch.cjs && docker pull -q $DASHBOARD_IMAGE >/dev/null && docker run --rm \
    -e VITE_API_URL=$api -e VITE_PUBLIC_API_URL=https://$DOMAIN/api \
    -v $APP_DIR/dashboard-patch:/patch:ro -v $WEB_DIR/registry:/out \
    --entrypoint sh $DASHBOARD_IMAGE -c 'node /patch/dashboard-patch.cjs && npx vite build --base=$base --outDir /tmp/dist --logLevel error \
      && cp LICENSE /tmp/dist/LICENSE.txt && rm -rf /out/* && cp -r /tmp/dist/. /out/'"
}

urls() {
  local dashboard=https://$DOMAIN/
  if [ "$ADMIN" = tunnel ]; then
    dashboard="http://localhost:3310 (through the SSH tunnel)"
  elif [ "$STORE" = on ]; then
    dashboard=https://$DOMAIN/registry/
  fi
  if [ "$STORE" = on ]; then
    echo "Store: https://$DOMAIN/  Dashboard: $dashboard"
  else
    echo "Dashboard: $dashboard"
  fi
}

# The backend's operator commands (DPAppRegistry account|usage …). A --file goes
# along on stdin, since the command runs inside the container.
operator() {
  local args=() file=""
  while [ $# -gt 0 ]; do
    if [ "$1" = "--file" ] && [ $# -gt 1 ]; then
      file=$2
      args+=(--file /dev/stdin)
      shift 2
    else
      args+=("$1")
      shift
    fi
  done
  local command="cd $APP_DIR && docker compose exec -T backend DPAppRegistry $(printf '%q ' "${args[@]}")"
  if [ -n "$file" ]; then
    remote "$command" <"$file"
  else
    remote "$command" </dev/null
  fi
}

restore_test() {
  remote 'bash -s' <<'EOF'
set -euo pipefail
cd /opt/dpappregistry
latest=$(ls -1t /var/backups/dpappregistry/mongo-*.archive.gz 2>/dev/null | head -1)
if [ -z "$latest" ]; then
  echo "No backup yet; run deploy/deploy.sh backup first." >&2
  exit 1
fi
name=dpappregistry-restore-test
docker rm -fv "$name" >/dev/null 2>&1 || true
docker run -d --rm --name "$name" mongo:7.0.12 >/dev/null
# -v: removing a --rm container by hand leaves its anonymous volumes behind.
trap 'docker rm -fv dpappregistry-restore-test >/dev/null 2>&1' EXIT
for attempt in $(seq 60); do
  docker exec "$name" mongosh --quiet --eval 'db.runCommand({ ping: 1 }).ok' </dev/null 2>/dev/null | grep -q 1 && break
  sleep 1
done
docker exec -i "$name" mongorestore --quiet --archive --gzip <"$latest"
count='const d = db.getSiblingDB("dpappregistry"); d.getCollectionNames().sort().forEach((c) => print(c + " " + d.getCollection(c).countDocuments()))'
docker exec "$name" mongosh --quiet --eval "$count" </dev/null >/tmp/dpappregistry-restored.txt
docker compose exec -T db sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval "$0"' "$count" </dev/null >/tmp/dpappregistry-live.txt
echo "Restored $(basename "$latest") into a scratch MongoDB: $(wc -l </tmp/dpappregistry-restored.txt) collections."
if diff -u /tmp/dpappregistry-live.txt /tmp/dpappregistry-restored.txt; then
  echo "Every collection holds as many documents as the live database."
else
  echo "The counts above differ: writes made since the backup show up here."
fi
EOF
}

if [ ! -f "$KEY" ]; then
  echo "No SSH key at $KEY." >&2
  exit 1
fi

case "${1:-deploy}" in
  deploy)
    setup
    certificate
    setup
    stack
    pages
    urls
    ;;
  web)
    setup
    certificate
    setup
    pages
    urls
    ;;
  store)
    if [ "$STORE" != on ]; then
      echo "There is no store page: STORE=on serves one." >&2
      exit 1
    fi
    store_page
    echo "Store: https://$DOMAIN/"
    ;;
  logs)
    remote "cd $APP_DIR && docker compose logs -f --tail=100 backend"
    ;;
  backup)
    remote "$APP_DIR/backup.sh" </dev/null
    ;;
  restore-test)
    restore_test
    ;;
  account | usage | license | order)
    operator "$@"
    ;;
  *)
    sed -n '2,/^$/p' "$0" | cut -c 3- >&2
    exit 64
    ;;
esac
