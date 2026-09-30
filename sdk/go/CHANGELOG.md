# Changelog

## v0.2.0

- `updatecheck`: update checks for services and command-line tools: scheduled and on-demand checks that report the newer release, leaving how to tell the user to the program.
- `appregistry.IsRelease`: whether a version can be checked for updates. `desktopupdate.IsRelease` now calls it.

## v0.1.0

- `appregistry`: client for DPAppRegistry update checks, with staged rollouts, edge fallback and download tokens for private apps.
- `desktopupdate`: a desktop app's update flow: scheduled and on-demand checks, a prompt, and the platform's download.
