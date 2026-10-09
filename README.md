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
- A public store page that lists every app that isn't private, with the installers of its latest release.

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
  "version": "1.3.0",
  "critical": false,
  "changelog": "…",
  "update_url_dmg": "https://…/myapp-1.3.0.dmg",
  "update_url_zip": "https://…/myapp-1.3.0.zip"
}
```

Each downloadable package appears as `update_url_<extension>`, and `version` names the release they belong to. Without a newer version, `update_available` is `false`; a response that still carries links, such as one offering a rollback, names their release too.

## The store

`GET /store/apps` is public. It lists the apps that aren't private, each with the latest release published on a channel and that release's installers (updater feed files are left out):

```
GET /store/apps?channel=stable&owner=admin
```

```json
{
  "apps": [
    {
      "name": "dpfs",
      "description": "…",
      "logo": "https://…/dpfs.png",
      "version": "1.3.0",
      "channel": "stable",
      "updated_at": "2026-10-01T09:00:00Z",
      "changes": "…",
      "downloads": [
        { "platform": "darwin", "arch": "arm64", "package": "zip", "url": "https://…/dpfs-1.3.0-darwin-arm64.zip", "size": 9123456, "sha256": "…" }
      ]
    }
  ]
}
```

`channel` defaults to `stable`, and `owner` to `STORE_OWNER`, then `admin`. Apps with nothing published on the channel are left out, and so are update metadata files (blockmaps, `latest*.yml`, signatures). With `owner` given, the answer also names the `account` and, on a plan with store branding, carries its `profile`; an account that doesn't exist is a 404.

[`store/`](store) is a page for people built on it: the latest release, every app with its installer for the visitor's system first, sizes and SHA-256 checksums, install steps per system, in Vietnamese or English. It is static; [`store/config.js`](store/config.js) sets its name, logo, texts, links and the API's address. At `/@<account>/` the same page shows one account's apps, under that account's profile when its plan allows.

## Deploying to a server

[`deploy/`](deploy) runs everything on one domain of an Ubuntu or Debian server, behind the server's nginx:

| Path | What |
|---|---|
| `/` | the store, for users |
| `/registry/` | the dashboard, for admins |
| `/api/` | the API: update checks and uploads |
| `/files/` | public builds |

```bash
DOMAIN=store.example.com DEPLOY_HOST=203.0.113.10 deploy/deploy.sh
```

The domain has to point at the server first: the script gets its HTTPS certificate from Let's Encrypt. It installs Docker, starts the API, MongoDB, Redis and Garage with [`deploy/compose.yaml`](deploy/compose.yaml) (none of them reachable from outside), runs the migrations, and builds the dashboard for this API under `/registry/`. Secrets are made on the server on the first run, in `/opt/dpappregistry/.env`, and kept. Run the same command again to deploy a new version.

The first admin signs up in the dashboard with the server's `API_KEY`. Apps they create appear in the store once a release is published.

Every night at 03:15 the server backs up MongoDB to `/var/backups/dpappregistry/` (the newest 14 are kept) and mirrors the stored files; [`deploy/backup.sh`](deploy/backup.sh) has the details. `deploy/deploy.sh backup` backs up now, and `deploy/deploy.sh restore-test` restores the newest backup into a scratch MongoDB and compares it with the live one.

## Plans

A hosted registry can sell accounts on plans ([`server/plans`](server/plans)): Hobby, Starter, Team and Business, which cap apps, storage, members and the monthly active installs included, and include features such as private apps, staged rollouts, statistics, crash reports, TUF and store branding. They are enforced only when `PLANS_ENABLED=true`; a self-hosted registry is unlimited. Past a limit the API answers `402` with `"code": "plan_limit"`, and the dashboard's Settings → Plan shows each account's plan and usage.

Monthly active installs are the installs that checked for an update in the month: the `X-Device-ID` the SDKs send (each install makes a random one), counted in a Redis HyperLogLog per app and month. Checks without one count as estimates. Update checks are never refused; a Hobby account past its installs only stops uploading.

Accounts and plans are set with the backend's operator commands, through `deploy/deploy.sh` on a deployed server:

```bash
deploy/deploy.sh account create acme --plan starter          # prints the account's password once
deploy/deploy.sh account plan acme business --mai 400000      # limits given override the plan's
deploy/deploy.sh account store acme --file acme-profile.json  # the store page's name, texts, logo, links
deploy/deploy.sh account list
deploy/deploy.sh usage report --month 2026-10                 # installs, overage and storage, for invoices
```

## Selling licenses

A registry can sell its products: the store shows prices, takes an order, and once the bank transfer arrives issues a license key the app verifies offline.

- **Keys** are `dpl1.<payload>.<signature>`: the base64url JSON of the license (product, plan, customer, servers, dates) signed with the registry's Ed25519 key (`LICENSE_SIGNING_KEY`, a 32-byte seed in hex that `deploy/server-setup.sh` makes once). Apps verify with the public key from `license pubkey`, using [`sdk/go/licensekey`](sdk/go/licensekey) or the JS SDK's `parseLicense`; nothing is trusted before the signature checks. `/checkVersion` now also says `published_at`, so an app can tell whether a release falls inside its license's updates window.
- **The catalog** is in [`server/license/catalog.go`](server/license/catalog.go) and served by `GET /store/products`.
- **Orders**: `POST /store/orders` starts one and answers with the amount, a payment code (`MTB…`) for the transfer's content, a VietQR image and the bank details; `GET /store/orders/:id?t=<token>` is the buyer's view, with the key once paid. SePay posts each incoming transfer to `POST /webhooks/sepay`; the registry matches the code, marks the order paid, issues the license, shows it on the order page and emails it when SMTP is configured. A transfer is acted on once however often it is delivered, and `order reconcile` reads SePay's transaction list for webhooks that never arrived.
- **By hand**: `license issue` for a contract or a transfer confirmed outside the store; `order fulfil` for an order paid another way.
- **Activation**: an app may `POST /store/licenses/activate` with its key and cluster id; the registry records which clusters run under a license and answers whether it is active, expired or revoked. This informs: the key stays valid offline.

```bash
deploy/deploy.sh license pubkey                        # the public key apps are built with
deploy/deploy.sh license issue --product dpfs --plan business --customer "ABC" --email it@abc.vn --clusters 2
deploy/deploy.sh license list
deploy/deploy.sh license revoke <id> --reason "refund"
deploy/deploy.sh order list
deploy/deploy.sh order reconcile --dry-run
```

Settings: `ORDERS_ENABLED`, `SEPAY_BANK_ACCOUNT`, `SEPAY_BANK_NAME`, `SEPAY_ACCOUNT_HOLDER`, `SEPAY_WEBHOOK_SECRET` (HMAC, preferred) or `SEPAY_WEBHOOK_APIKEY`, `SEPAY_API_KEY`, `SMTP_*`, `STORE_URL`, `PLANS_CONTACT`; see `.env.example`. The SePay webhook URL is `https://<store>/api/webhooks/sepay`.

## SDKs

Apps normally check through the SDKs in [`sdk/`](sdk), which are versioned with this API:

| Package | For | Helpers |
|---|---|---|
| [`sdk/go`](sdk/go): `github.com/DacPhu/DPAppRegistry/sdk/go/appregistry` | Go apps, services and tools | [`desktopupdate`](sdk/go/desktopupdate): scheduled and on-demand checks, a prompt, and the download, for Wails, Fyne and other toolkits. [`updatecheck`](sdk/go/updatecheck): the same checks for a service or command-line tool, reported to the program instead of prompted |
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
