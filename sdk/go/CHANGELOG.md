# Changelog

## v0.3.0

- `appregistry.UpdateResponse.Version`: the version of the release a response describes, from the server's new `version` field.
- `updatecheck.Update.Version` carries it, and `desktopupdate` names it in its offer ("My App 1.3.0 is available").
- Shorter errors: a failed check names its URL once and drops the repeated prefix, and a failed response now carries the reason the server gave (`… returned HTTP 400: <reason>`), which used to be discarded.

## v0.2.0

- `updatecheck`: update checks for services and command-line tools: scheduled and on-demand checks that report the newer release, leaving how to tell the user to the program.
- `appregistry.IsRelease`: whether a version can be checked for updates. `desktopupdate.IsRelease` now calls it.

## v0.1.0

- `appregistry`: client for DPAppRegistry update checks, with staged rollouts, edge fallback and download tokens for private apps.
- `desktopupdate`: a desktop app's update flow: scheduled and on-demand checks, a prompt, and the platform's download.
