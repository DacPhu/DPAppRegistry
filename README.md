# DPAppRegistry

DPAppRegistry is a self-hosted update server for desktop applications. You upload each release's builds; installed apps ask the server whether a newer version exists for their platform, architecture and release channel, and receive download links or the feed their updater framework expects.

## Features

- Multiple apps, each with release channels (for example stable, beta, nightly), platforms and architectures.
- Native update feeds for electron-builder, Tauri, Squirrel.Mac, Squirrel.Windows, Sparkle and Velopack, and plain download links for apps without an updater framework.
- Staged rollouts, critical updates and changelogs.
- Private apps with download tokens.
- Storage on AWS S3, Google Cloud Storage, DigitalOcean Spaces, or Garage for local development.
- Optional TUF (The Update Framework) metadata signing, crash and event reports, telemetry and Slack notifications.
- A web dashboard for managing apps and releases.

## Requirements

- Go 1.26 or later, to build from source
- MongoDB and Redis
- A public and a private bucket on S3-compatible storage

Docker Compose provides all of them for local use.

## Quick start

```bash
docker compose up --build
docker compose exec -T backend /usr/bin/DPAppRegistry migrate up   # once the stack is healthy
```

The API listens on `http://localhost:9000` and the dashboard on `http://localhost:3000`. The stack also runs MongoDB, Redis and Garage (S3-compatible storage); Garage's web UI and credentials are defined in `docker-compose/services/s3.yml`.

To keep the dependencies in Docker and run the API from source:

```bash
docker compose -f docker-compose.yaml -f docker-compose.development.yaml up
go build -o DPAppRegistry DPAppRegistry.go
./DPAppRegistry migrate up
./DPAppRegistry
```

## Publishing releases

Create an app with its channels, platforms and architectures in the dashboard or through the API, then upload each release's builds with its version. Large builds can go straight to storage through presigned URLs: `POST /upload/init`, a `PUT` to each returned URL, then `POST /upload/complete`.

Each platform has an updater type, which decides what its clients receive: electron-builder, Tauri, Squirrel, Sparkle and Velopack clients get their framework's feed; other clients get JSON with download links.

[`examples/DPAppRegistry.postman_collection.json`](examples/DPAppRegistry.postman_collection.json) covers the full API.

## Checking for updates

Clients call `GET /checkVersion` with their identity and current version:

```
GET /checkVersion?owner=admin&app_name=myapp&version=1.2.0&channel=stable&platform=darwin&arch=arm64
```

```json
{
  "update_available": true,
  "critical": false,
  "changelog": "…",
  "update_url_dmg": "https://…/myapp-1.3.0.dmg",
  "update_url_zip": "https://…/myapp-1.3.0.zip"
}
```

Each downloadable package appears as `update_url_<extension>`. Without a newer version, `update_available` is `false`.

## SDKs

Apps normally check through the SDKs in [`sdk/`](sdk), which are versioned with this API:

| Package | For | Desktop helper |
|---|---|---|
| [`sdk/go`](sdk/go): `github.com/DacPhu/DPAppRegistry/sdk/go/appregistry` | Go apps | [`desktopupdate`](sdk/go/desktopupdate): scheduled and on-demand checks, a prompt, and the download, for Wails, Fyne and other toolkits |
| [`sdk/js`](sdk/js): `@dacphu/dpappregistry-sdk` | Node.js and Electron | [`/electron`](sdk/js#electron-apps): hands electron-builder feeds to electron-updater, or opens the download |

The server's end-to-end suite runs against `sdk/go`, so an API change that breaks the Go SDK fails here first. Release the Go SDK by tagging `sdk/go/vX.Y.Z` (it is a nested module) and the JavaScript SDK with `npm publish` from `sdk/js`.

## Configuration

The server reads environment variables, or a `.env` file in its working directory. [`.env.example`](.env.example) documents every variable; `.env.local` holds working values for the Docker Compose stack.

| Area | Variables |
|---|---|
| Server | `PORT` (default `9000`), `API_URL`, `ALLOWED_CORS`, `TRUSTED_PROXIES` |
| Security | `API_KEY` (required to sign up), `JWT_SECRET` |
| Database | `MONGODB_URL` |
| Redis (required) | `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, `REDIS_DB` |
| Storage | `STORAGE_DRIVER` (`aws`, `gcp`, `digitalocean`, or the deprecated `minio`), `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION`, `S3_BUCKET_NAME`, `S3_BUCKET_NAME_PRIVATE`, `S3_ENDPOINT`, `S3_ENDPOINT_PRIVATE`, `S3_API_ENDPOINT` |
| Optional features | `PERFORMANCE_MODE`, `ENABLE_TELEMETRY`, `TUF_ENABLED` with `ONLINE_KEY_DIR`, `REPORTS_ENABLED`, `SLACK_ENABLE` with the other `SLACK_*` variables |

Set `TRUSTED_PROXIES` to your reverse proxies' addresses, or to `127.0.0.1` when the API is exposed directly. Left unset, any client can spoof its IP address past the login and sign-up rate limits.

For local storage, use Garage with the `aws` driver, as the Compose stack does. MinIO still works but is deprecated.

## Testing

```bash
go test ./server/... ./mongod/...            # server unit tests
(cd sdk/go && go test ./...)                 # Go SDK
(cd sdk/js && npm install && npm test)       # JavaScript SDK
```

The end-to-end suite (`go test .`) runs against a live stack and needs a populated `.env`. With Docker Compose running:

```bash
docker exec -it DPAppRegistry_backend /usr/bin/DPAppRegistry_tests
```

## Database migrations

Migrations live in `mongod/migrations`. `./DPAppRegistry migrate up` applies them and `migrate down` rolls back. Create a new one with [golang-migrate](https://github.com/golang-migrate/migrate/blob/master/cmd/migrate/README.md):

```bash
cd mongod/migrations
migrate create -ext json name_of_migration
```

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
