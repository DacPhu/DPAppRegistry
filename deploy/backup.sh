#!/usr/bin/env bash
# Backs up DPAppRegistry on its server. dpappregistry-backup.timer runs it every
# night; deploy/deploy.sh backup runs it now.
#
#   1. Every account's usage this month is saved into MongoDB, so the dump keeps
#      the history invoices are made from.
#   2. MongoDB is dumped to /var/backups/dpappregistry/mongo-<time>.archive.gz;
#      the newest 14 are kept.
#   3. While the files live in Garage, both buckets are mirrored to
#      /var/backups/dpappregistry/files/.
#   4. Once R2 is set up (R2_ACCOUNT_ID and its keys in .env), each dump is also
#      copied to the R2 bucket dpappregistry-backups, off this server.

set -euo pipefail

APP_DIR=/opt/dpappregistry
BACKUP_DIR=/var/backups/dpappregistry
KEEP=14

cd "$APP_DIR"
set -a
. ./.env
set +a
umask 077
mkdir -p "$BACKUP_DIR"

docker compose exec -T backend DPAppRegistry usage snapshot </dev/null

stamp=$(date -u +%Y%m%dT%H%M%SZ)
dump="$BACKUP_DIR/mongo-$stamp.archive.gz"
# The credentials are the container's own; written aside so a failed dump never
# looks like a finished one.
docker compose exec -T db sh -c 'exec mongodump --quiet --archive --gzip \
  -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin' </dev/null >"$dump.part"
mv "$dump.part" "$dump"
ls -1t "$BACKUP_DIR"/mongo-*.archive.gz | tail -n +$((KEEP + 1)) | xargs -r rm --

if [ -n "${R2_ACCOUNT_ID:-}" ]; then
  docker run --rm -v "$BACKUP_DIR:/backup:ro" \
    -e RCLONE_CONFIG_R2_TYPE=s3 -e RCLONE_CONFIG_R2_PROVIDER=Cloudflare \
    -e RCLONE_CONFIG_R2_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" -e RCLONE_CONFIG_R2_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
    -e RCLONE_CONFIG_R2_ENDPOINT="https://$R2_ACCOUNT_ID.r2.cloudflarestorage.com" \
    rclone/rclone:latest copyto "/backup/$(basename "$dump")" "r2:dpappregistry-backups/mongo/$(basename "$dump")"
  where="this server and R2"
else
  for bucket in dpappregistry-public dpappregistry-private; do
    docker run --rm --network host -v "$BACKUP_DIR/files:/backup" \
      -e RCLONE_CONFIG_GARAGE_TYPE=s3 -e RCLONE_CONFIG_GARAGE_PROVIDER=Other \
      -e RCLONE_CONFIG_GARAGE_ACCESS_KEY_ID="$GARAGE_ACCESS_KEY" -e RCLONE_CONFIG_GARAGE_SECRET_ACCESS_KEY="$GARAGE_SECRET_KEY" \
      -e RCLONE_CONFIG_GARAGE_ENDPOINT=http://127.0.0.1:3900 -e RCLONE_CONFIG_GARAGE_REGION=us-east-1 \
      -e RCLONE_CONFIG_GARAGE_FORCE_PATH_STYLE=true \
      rclone/rclone:latest sync --quiet "garage:$bucket" "/backup/$bucket"
  done
  where="this server"
fi

echo "Backed up MongoDB ($(du -h "$dump" | cut -f1)) to $where: $dump"
