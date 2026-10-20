# Changelog

Release headings use `## vX.Y.Z`: the release workflow tags the first one it finds.

## Unreleased

- Update server for desktop apps: apps, channels, platforms and architectures; staged rollouts; critical updates; private apps with download tokens; optional TUF metadata signing; crash and event reports.
- Update feeds for electron-builder, Tauri, Squirrel.Mac, Squirrel.Windows, Sparkle and Velopack, and download links for other apps.
- SDKs in `sdk/`: Go (`appregistry`, `desktopupdate`) and JavaScript (`@dacphu/dpappregistry-sdk`, with an Electron helper).
- Prometheus metrics on a listener of their own (`METRICS_ADDR`, open to `METRICS_ALLOW` only): requests by route template and status, their durations, update checks by app and result, license activations by result.
- Log lines in one format for a log shipper, to stderr and, with `LOG_DIR`, a file per day kept 14 days (a file that can't be written is tried again every minute). The access log and the panic log replace gin's and never write query strings or headers.
- `deploy/`: the dashboard and signing in only through an SSH tunnel by default (`ADMIN=tunnel`; `ADMIN=public` puts the dashboard back on the domain). The metrics port and the log volume for the infra repo's Prometheus and Alloy, and a JSON access log for the public site. The upstream dashboard image is pinned by digest.
