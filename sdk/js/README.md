# DPAppRegistry JS SDK

Production-oriented JavaScript/TypeScript SDK for checking application updates with DPAppRegistry.

This package is a small typed transport and developer experience layer. It does not implement update installation, platform normalization, metadata verification, caching, or business rules. For an Electron app's whole update flow, see [Electron apps](#electron-apps).

## Electron apps

`@dacphu/dpappregistry-sdk/electron` runs the update flow every Electron product shares. It checks 30 seconds after `start()` and every 6 hours after that, and `checkNow()` runs a check on demand. When a release carries an electron-builder feed (its `latest*.yml` package) and this install can replace itself (Windows, a Linux AppImage, or a Developer ID signed Mac), electron-updater downloads and installs it. Otherwise the platform's download opens in the browser. A declined update isn't offered again in the background unless it's marked critical.

```ts
import { app, dialog, shell, Menu } from 'electron';
import { autoUpdater } from 'electron-updater';
import { Client } from '@dacphu/dpappregistry-sdk';
import { createElectronUpdater } from '@dacphu/dpappregistry-sdk/electron';

const updater = createElectronUpdater({
  client: new Client({ baseURL: 'https://registry.example.com' }),
  check: { owner: 'admin', appName: 'my-app', channel: 'stable' }, // version, platform, arch default to the running app
  productName: 'My App',
  electron: { app, dialog, shell },
  autoUpdater,
});

app.whenReady().then(() => updater.start());
// In a menu: { label: 'Check for Updates…', click: () => void updater.checkNow() }
```

The package depends on neither `electron` nor `electron-updater`: the app passes its own. On DPAppRegistry, register the app's platforms with the `electron-builder` updater and upload electron-builder's output for each release: its installers, blockmaps and `latest*.yml`. The installers need names without spaces, because `latest*.yml` names files with dashes.

## Requirements

Node.js 18 or later.

## Installation

```sh
npm install @dacphu/dpappregistry-sdk
```

## Quick Start

```ts
import { Client } from '@dacphu/dpappregistry-sdk';

const client = new Client({
  baseURL: 'https://api.example.com',
});

const resp = await client.checkForUpdates({
  owner: 'admin',
  appName: 'test',
  version: '0.0.0.5',
  channel: 'nightly',
  platform: 'darwin',
  arch: 'arm64',
});

if (resp.updateAvailable) {
  if (resp.updateUrl !== '') {
    console.log('Update available:', resp.updateUrl);
  }
  for (const pkg of resp.packageUrls) {
    console.log(`${pkg.package} update available:`, pkg.url);
  }
}
```

## Configuration

```ts
import { Client } from '@dacphu/dpappregistry-sdk';

const client = new Client({
  baseURL: 'https://api.example.com',
  edgeURL: 'https://cdn.example.com',
  timeoutMs: 10_000,
  fetch: customFetch,
});
```

`baseURL` is required. It points to the DPAppRegistry API.

`edgeURL` is optional. When configured, the SDK tries a static edge JSON response before falling back to the API.

`timeoutMs` is optional. Defaults to `30000`. Applied to every request.

`fetch` is optional. When omitted, the SDK uses `globalThis.fetch` (Node.js 18+ built-in). Supply a custom function to configure proxies, connection pooling, or logging.

The client is safe for concurrent use.

## Update Checks

`checkForUpdates` returns a typed response:

```ts
const resp = await client.checkForUpdates({
  owner: 'admin',
  appName: 'test',
  version: '0.0.0.5',
  channel: 'nightly',
  platform: 'darwin',
  arch: 'arm64',
  deviceId: 'optional-device-id',
});
```

The SDK only validates `owner`, `appName`, and `version` as required. `channel`, `platform`, and `arch` are not validated client-side and are sent as empty query values when omitted — but if the target app in your DPAppRegistry instance defines channels, platforms, or architectures, you must provide the matching values or the check will not resolve to the correct release.

`deviceId` is optional. When set, the SDK sends it as the `X-Device-ID` header and triggers a telemetry beacon after a successful edge response.

`downloadToken` is needed only for a private app in `strict` mode. The SDK sends it as the `X-Download-Token` header; such an app answers a check without it exactly as it answers a check for an unknown app. The token is scoped to one app and channel. When `downloadToken` is set, `edgeURL` is skipped: a private app is never published to the edge, so the lookup could only miss.

If your application downloads the artifact itself, send the same header to the `/download` link (the name is exported as `DOWNLOAD_TOKEN_HEADER`). `/download` answers with a redirect to presigned storage, and `fetch` forwards custom headers across hosts, so request it with `redirect: 'manual'` and fetch the `Location` URL without the header — otherwise the token reaches the storage provider and its access logs.

The SDK always uses the `manual` updater: it sends `updater=manual` on the `baseURL` API request and uses `manual` as the edge path segment. This is the native DPAppRegistry response that carries the full metadata (`critical`, `changelog`, `is_intermediate_required`, `possible_rollback`) and the per-package URLs (`update_url_yml`, `update_url_zip`, `update_url_dmg`). Framework-specific updater modes (e.g. `electron-builder`) return their own feed format and are meant to be consumed directly by that framework — point it at the `update_url_yml` from `packageUrls` instead.

An optional `AbortSignal` can be passed as the second argument to cancel the request:

```ts
const controller = new AbortController();
setTimeout(() => controller.abort(), 5_000);

const resp = await client.checkForUpdates(opts, controller.signal);
```

## Staged Rollout

DPAppRegistry can ship a version to a controlled percentage of the fleet first (a staged/canary rollout). When the offered version's rollout is below 100%, `/checkVersion` includes a `rollout` object and the SDK decides — client-side — whether this install is included:

```json
{
  "update_available": true,
  "update_url": "https://downloads.example.com/app",
  "rollout": { "percent": 20, "seed": "badadc23b08e3943" }
}
```

The decision is deterministic and sticky, using the reference algorithm shared by every DPAppRegistry SDK:

```text
bucket = sha256(deviceId + ":" + seed) → first 8 bytes, big-endian uint64, % 100
included if bucket < rollout.percent
```

When the install is **not** in the bucket, the SDK forces `updateAvailable` to `false` **and blanks `updateUrl`/`packageUrls`** — so a caller that inspects the URLs instead of `updateAvailable` still cannot pull an update the device was not offered. The `rollout` object on the response exposes the decision for logging:

```ts
const resp = await client.checkForUpdates({ ...opts, deviceId: 'stable-device-id' });

if (resp.rollout) {
  console.log(resp.rollout.percent, resp.rollout.bucket, resp.rollout.eligible);
}
```

`deviceId` is required to participate: it must be the same stable value used for telemetry (`X-Device-ID`). Without it the bucket cannot be computed, so the install stays out of the rollout (`eligible: false`, `bucket: null`) until a `deviceId` is provided. Raising the percentage on the same version only ever adds installs — an install in the 20% bucket stays in when you move to 50%. Rollout works identically in edge/CDN mode, since the same JSON body is served from the cached manifest.

The `rolloutBucket(deviceId, seed)` helper is exported if you need to compute a bucket yourself.

## Native Updater Feeds

Framework-native updaters (Squirrel.Mac, Squirrel.Windows) poll DPAppRegistry themselves with a different `updater` value and URL shape than the SDK's `manual` check. The SDK owns that wire format so you only pick the updater.

### `resolveNativeFeed` (recommended)

`resolveNativeFeed` resolves the feed edge-first (falling back to the API) and tells you whether to invoke the native updater at all. When an edge response exists, the returned `feedURL` points at the CDN, so the native updater reads it directly and the API is never hit:

```ts
const feed = await client.resolveNativeFeed({
  owner: 'admin',
  appName: 'test',
  version: '0.0.1',
  channel: 'nightly',
  platform: 'darwin',
  arch: 'arm64',
  updater: 'squirrel_darwin', // or 'squirrel_windows'
});

if (feed.updateAvailable) {
  autoUpdater.setFeedURL({ url: feed.feedURL });
  autoUpdater.checkForUpdates();
}
```

`feed.source` is `'edge'` or `'api'`. Why this matters: Squirrel.Mac expects `200 { "url": "<zip>" }` or `204 No Content`, but the edge mirror returns `200 { "status": "no_content" }` (not 204) when there is no update, and `404` until the API has warmed that version's edge object. The SDK handles both — it reads the edge response, returns `updateAvailable: false` on `no_content` so you skip the native updater entirely, and falls back to the API (which warms the edge) on a miss. Pointing the native updater straight at the edge URL yourself would break on those cases.

`feed.feedURL` is what to pass to `setFeedURL`, and it differs by framework:

- `squirrel_darwin` reads the JSON feed directly, so `feedURL` is the edge object (or the API `/checkVersion` URL on a miss).
- `squirrel_windows` reads `feedURL/RELEASES`, so the SDK resolves the edge response (`{ "status": "redirect", "url": "<.../RELEASES>" }`), strips the trailing `/RELEASES`, and returns the **directory** as `feedURL` — pointing Squirrel.Windows straight at the CDN where `RELEASES` and the `.nupkg` live. On an edge miss it falls back to the API `/update/...` base (which serves `RELEASES` and warms the edge). `feed.url` carries the raw resolved resource (the `.zip` for darwin, the `RELEASES` URL for windows).

### `buildNativeFeedURL` (low-level)

Builds just the feed URL (no request, no edge fallback) when you want to wire the native updater yourself:

```ts
const url = client.buildNativeFeedURL({ owner, appName, version, channel, platform, arch, updater: 'squirrel_darwin' });
```

- `squirrel_darwin` → `GET {baseURL}/checkVersion?...&updater=squirrel_darwin`
- `squirrel_windows` → `{baseURL}/update/{owner}/{app}/{channel}/{platform}/{arch}/{version}`; Squirrel.Windows appends `/RELEASES` itself.

For both methods `owner`, `appName`, `version`, `platform`, and `arch` are required; an unknown `updater` throws `UnsupportedUpdaterError`. The supported values are exported as `NATIVE_UPDATERS`.

## Reports

`reportEvent` posts a failure or diagnostic report to `POST /reports/ingest`. The client stays stateless and app-agnostic, so `reportKey` and `deviceId` are passed per call (like `checkForUpdates`), not stored in `Config`.

```ts
const resp = await client.reportEvent({
  reportKey: 'rpk_...',        // sent as Authorization: Bearer
  deviceId: 'device-1',        // required, sent as X-Device-ID
  appName: 'test',
  version: '0.0.0.5',
  channel: 'nightly',
  platform: 'darwin',
  arch: 'arm64',
  event: { type: 'crash', reason: 'segfault.signal-11' },
  details: { stack: '...', exitCode: 11 }, // optional raw debug object
});

console.log(resp.status, resp.groupHash, resp.storedDetails);
```

`event.type` must be one of `crash`, `startup_failure`, `update_failure`, `install_failure`, or `rollback_failure`. `event.reason` must match `^[a-zA-Z0-9._-]{1,128}$`.

`reportKey`, `deviceId`, `appName`, `version`, `channel`, `platform`, and `arch` are all required and validated client-side.

`details` is optional. When supplied, the SDK serializes it with `JSON.stringify`, gzip-compresses it, and base64-encodes the result. The request carries it as:

```json
{
  "details": {
    "encoding": "gzip+base64",
    "content_type": "application/json",
    "payload": "<base64(gzip(json))>"
  }
}
```

When `details` is omitted, the `details` field is left out of the request entirely.

A successful request returns HTTP `202` with a mapped `ReportResponse`:

```json
{ "status": "accepted", "group_hash": "...", "stored_details": false }
```

Any non-`202` response rejects with an `EndpointError` whose `source` is `'report'` and `statusCode` holds the HTTP status (e.g. `401`, `403`, `429`). Network and JSON failures are wrapped in `EndpointError` as well.

An optional `AbortSignal` can be passed as the second argument to cancel the request.

## Base API Request

The `baseURL` API request uses `GET /checkVersion`:

```
GET /checkVersion?app_name=test&version=0.0.0.5&channel=nightly&platform=darwin&arch=arm64&owner=admin&updater=manual
X-Device-ID: optional
X-Download-Token: optional
```

## Response Model

DPAppRegistry may return a direct binary update URL:

```json
{
  "update_available": true,
  "update_url": "https://downloads.example.com/app"
}
```

It may also return package-specific URLs with dynamic field names:

```json
{
  "update_available": true,
  "update_url_deb": "https://downloads.example.com/app.deb",
  "update_url_rpm": "https://downloads.example.com/app.rpm",
  "changelog": "### Changelog\n\n- Added feature X",
  "critical": true,
  "is_intermediate_required": true,
  "possible_rollback": true
}
```

When a version is under a staged rollout, the response also carries a `rollout` object (`{ percent, seed }`), decoded into `resp.rollout` — see [Staged Rollout](#staged-rollout).

The SDK decodes these into a typed `UpdateResponse`:

```ts
if (resp.updateUrl !== '') {
  console.log(resp.updateUrl);
}

for (const pkg of resp.packageUrls) {
  console.log(pkg.package, pkg.url);
}
```

`packageUrls` is always sorted alphabetically by package name.

`source` identifies where the response came from: `'edge'`, `'api'`, or `'unknown'`.

## EdgeURL Fallback

When `edgeURL` is configured, the SDK first tries a static JSON response:

```
GET /responses/{owner}/{appName}/{channel}/{platform}/{arch}/manual/{version}.json
```

For example:

```
GET /responses/admin/test/nightly/darwin/arm64/manual/0.0.0.5.json
```

Empty `channel`, `platform` and `arch` are left out of the path, and the `manual` segment is dropped when `platform` is empty, matching the object key the server writes.

If the edge response succeeds with HTTP 200 and valid JSON, `resp.source` is `'edge'`.

The SDK falls back to the `baseURL` API when the edge request has:

- a network error
- a timeout
- invalid JSON
- HTTP 404
- any other non-200 response

If the fallback API succeeds, `resp.source` is `'api'`.

## Platform, Channel, and Architecture Values

DPAppRegistry supports fully custom platform, channel, and architecture values. This SDK never normalizes or remaps them.

The SDK will not change values such as:

- `macos` to `darwin`
- `osx` to `darwin`
- `stable` to `default`

Whatever string you pass in `channel`, `platform`, and `arch` is the string sent to DPAppRegistry.

## Optional System Helpers

The SDK provides optional helpers that return the current Node.js runtime values:

```ts
import { systemPlatform, systemArch } from '@dacphu/dpappregistry-sdk';

const platform = systemPlatform(); // process.platform — e.g. 'darwin', 'linux', 'win32'
const arch     = systemArch();     // process.arch    — e.g. 'arm64', 'x64'
```

These helpers are never called automatically. Use them only when Node.js runtime values match your DPAppRegistry configuration.

## Error Handling

The SDK validates required fields and throws typed errors:

```ts
import {
  Client,
  CheckError,
  EndpointError,
  ErrMissingBaseURL,
  ErrMissingOwner,
  ErrMissingAppName,
  ErrMissingVersion,
} from '@dacphu/dpappregistry-sdk';

try {
  const resp = await client.checkForUpdates(opts);
} catch (err) {
  if (err === ErrMissingBaseURL) {
    // configure Client baseURL
  } else if (err === ErrMissingOwner) {
    // set opts.owner
  } else if (err === ErrMissingAppName) {
    // set opts.appName
  } else if (err === ErrMissingVersion) {
    // set opts.version
  } else if (err instanceof CheckError) {
    // every edge/api attempt failed
    console.error('edge error:', err.edgeError?.message);
    console.error('api error:', err.apiError?.message);

    if (err.apiError instanceof EndpointError) {
      console.error('url:', err.apiError.endpointUrl);
      console.error('status:', err.apiError.statusCode);
    }
  }
}
```

A failed update check always rejects with a `CheckError`. Its `edgeError` and `apiError` fields hold the underlying `EndpointError` for each attempt (the edge error is absent when `edgeURL` is not configured).

### Error hierarchy

```
DPAppRegistryError
├── ValidationError      // ErrMissing* / ErrInvalid* singletons
└── RequestFailedError   // base class for any failed request
    ├── EndpointError    // a single edge/api request failed (has source, endpointUrl, statusCode)
    └── CheckError       // checkForUpdates failed (wraps edgeError / apiError)
```

If you only need to tell a failed request apart from a bad option, catch the base `RequestFailedError`:

```ts
import { Client, RequestFailedError } from '@dacphu/dpappregistry-sdk';

try {
  const resp = await client.checkForUpdates(opts);
} catch (err) {
  if (err instanceof RequestFailedError) {
    console.error('update check request failed:', err.message);
  } else {
    console.error('invalid update check options:', (err as Error).message);
  }
}
```

Validation errors (`ErrMissing*`, `ErrInvalid*`) are singleton instances — use `===` to check them. Request failures are class instances — use `instanceof` to narrow them.

## Examples

Runnable examples are available in:

- [`examples/basic`](examples/basic) — minimal setup using runtime platform/arch detection
- [`examples/edge-fallback`](examples/edge-fallback) — `edgeURL` configured with `deviceId` telemetry
- [`examples/custom-fetch`](examples/custom-fetch) — custom `fetch` function and `timeoutMs`
- [`examples/private-app`](examples/private-app) — `downloadToken` for a `strict` private app and a download that keeps the token out of storage requests

Run any example with:

```sh
npx ts-node examples/basic/index.ts
```

## Security Scope

This SDK version performs update-check transport requests and typed response decoding only.

It does not verify TUF metadata, signatures, thresholds, expiration, rollback protection, or cache safety. Applications that need secure update metadata verification must perform that verification in the appropriate DPAppRegistry component or a future SDK layer that explicitly implements it.

No signature, threshold, expiration, rollback, freeze, root-of-trust, or cache protection is weakened by this transport-only SDK.
