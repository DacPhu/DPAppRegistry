# Changelog

## v0.1.0

- Forked from `faynosync-sdk-go` v0.4.0 as module `github.com/DacPhu/DPAppRegistry/sdk/go`, package `appregistry`.
- Added `desktopupdate`: the shared desktop update flow (scheduled and on-demand checks, prompt, download).

## v0.4.0

### Added

- `CheckOptions.DownloadToken` for private apps whose download mode is `strict`: the SDK sends it as the `X-Download-Token` header, and such an app answers a check without it exactly as it answers a check for an unknown app. The token is scoped to one app and channel.
- `DownloadTokenHeader` and `StripDownloadTokenOnRedirect` for applications that fetch the artifact themselves: `/download` redirects to presigned storage, and Go forwards custom headers across hosts, so without the helper the token reaches the storage provider's logs.

### Changed

- `EdgeURL` is skipped when `DownloadToken` is set. A private app is never published to the edge, so the lookup could only miss and cost a request.

## v0.3.0

### Added

- Staged (canary) rollout support in `CheckForUpdates`: when `/checkVersion` returns a `rollout` object (`percent`, `seed`), the SDK computes a deterministic, sticky per-device bucket (`sha256(deviceID + ":" + seed)` → first 8 bytes big-endian uint64 `% 100`) and includes the install only when `bucket < percent`. Excluded installs get `UpdateAvailable: false` with `UpdateURL`/`PackageURLs` cleared so the gate cannot be bypassed. Exposes the decision via the new `RolloutInfo` (`Rollout` on `UpdateResponse`) and the `RolloutBucket` helper. Requires `DeviceID`; without it the install stays out of the rollout. Works in edge/CDN mode too

## v0.2.0

### Added

- "Updater" field to edge path.


## v0.1.0

Initial release of the DPAppRegistry Go SDK (transport layer only).

### Added

- `Client` with `CheckForUpdates` against the Base API (`GET /checkVersion`)
- Optional `EdgeURL` static JSON lookup with automatic API fallback
- Typed `CheckOptions`, `UpdateResponse`, and package URL decoding
- Sentinel errors and `EndpointError` for request failures
- Optional `SystemPlatform` / `SystemArch` helpers
- Examples: basic, edge fallback, custom HTTP client
- Add telemetry beacon to the edge response
