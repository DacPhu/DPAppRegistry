# Changelog

## 0.2.0

- `parseLicense`, `licenseExpired`, `licenseCoversUpdate`, `licenseAllowsCluster`, `licenseKeyId`: read the license keys a registry sells (`dpl1.<payload>.<signature>`, Ed25519), verified offline with the publisher's public key. Same format and test vector as the Go SDK's `licensekey`.
- `UpdateResponse.publishedAt`: when the release was published, from the server's new `published_at`.

- `@dacphu/dpappregistry-sdk/electron`: each install sends a random install ID as `X-Device-ID`, kept in the app's userData folder, so the registry can count active installs and staged rollouts include the install. A `deviceId` in `check` is sent instead; `DPAPPREGISTRY_NO_INSTALL_ID=1` turns it off. `installId(app)` is exported.

## 0.1.0

- Client for DPAppRegistry update checks, with staged rollouts, edge fallback, native feeds and crash reports.
- `@dacphu/dpappregistry-sdk/electron`: an Electron app's update flow: electron-builder feeds to electron-updater, otherwise the platform's download.
