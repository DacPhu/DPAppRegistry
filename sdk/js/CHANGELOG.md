# Changelog

## 0.1.0

- Forked from `@faynosync/sdk-js` 0.8.0 as `@dacphu/dpappregistry-sdk`.
- Added `@dacphu/dpappregistry-sdk/electron`: the shared Electron update flow (electron-builder feed to electron-updater, browser download otherwise).

## 0.8.0

### Fixed

- Edge path matches the server's object key: empty `channel`/`platform`/`arch` skipped.

## 0.7.0

### Added

- `downloadToken` in `CheckOptions` for private apps whose download mode is `strict`: the SDK sends it as the `X-Download-Token` header, and such an app answers a check without it exactly as it answers a check for an unknown app. The token is scoped to one app and channel.
- `DOWNLOAD_TOKEN_HEADER` for applications that fetch the artifact themselves.

### Changed

- `edgeURL` is skipped when `downloadToken` is set. A private app is never published to the edge, so the lookup could only miss and cost a request.

## 0.6.0

### Added

- Staged (canary) rollout support in `checkForUpdates`: when `/checkVersion` returns a `rollout` object (`percent`, `seed`), the SDK computes a deterministic, sticky per-device bucket (`sha256(deviceId + ":" + seed)` → first 8 bytes big-endian uint64 `% 100`) and includes the install only when `bucket < percent`. Excluded installs get `updateAvailable: false` with `updateUrl`/`packageUrls` blanked so the gate cannot be bypassed. Exposes the decision via the new `RolloutInfo` (`rollout` on `UpdateResponse`) and the `rolloutBucket` helper. Requires `deviceId`; without it the install stays out of the rollout. Works in edge/CDN mode too

## 0.5.0

### Added

- `Client.resolveNativeFeed` to resolve a native updater feed edge-first with API fallback, returning `NativeFeedResult` (`updateAvailable`, `feedURL`, `source`, `url`) so callers skip the native updater on no-update and keep the API offloaded when the edge is warm. `squirrel_windows` is edge-served too: the SDK reads the edge redirect response and returns the CDN `RELEASES` directory (trailing `/RELEASES` stripped) as `feedURL`, falling back to the API `/update/...` base on a miss

## 0.4.0

### Added

- `Client.buildNativeFeedURL` to build framework-native updater feed URLs (`squirrel_darwin`, `squirrel_windows`), with typed `NativeFeedOptions` / `NativeUpdater`, the `NATIVE_UPDATERS` list, and `UnsupportedUpdaterError`

## 0.3.0

### Added

- `reportEvent` for posting failure/diagnostic reports to `POST /reports/ingest`, with typed `ReportOptions` / `ReportResponse` and gzip+base64 `details` payloads
- Optional `deviceId` sent as `X-Device-ID`, including an edge telemetry beacon after successful edge responses
- New validation errors: `ErrMissingReportKey`, `ErrMissingDeviceId`, `ErrMissingChannel`, `ErrMissingPlatform`, `ErrMissingArch`, `ErrInvalidEventType`, `ErrInvalidReason`


## 0.2.1

### Fixed

- Normalize `-` to `.` in the edge version path segment so URLs like `2.0.0-4` resolve to the correct static response (matches Base API behavior)

## 0.2.0

Initial release of the DPAppRegistry JS SDK (transport layer only).

### Added

- `Client` with `checkForUpdates` against the Base API (`GET /checkVersion`)
- Optional `edgeURL` static JSON lookup with automatic API fallback
- Typed `CheckOptions`, `UpdateResponse`, and package URL decoding
- `EndpointError` and `CheckError` for typed request failure handling
- Sentinel validation errors for missing/invalid configuration
- Optional `systemPlatform` / `systemArch` helpers (Node.js `process.platform` / `process.arch`)
- `AbortSignal` support for request cancellation
- Custom `fetch` support for transport customization (`timeoutMs`, keep-alive, proxies)
- Examples: basic, edge fallback, custom fetch
