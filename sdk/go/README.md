# DPAppRegistry Go SDK

Production-oriented Go SDK for checking application updates with DPAppRegistry.

This package is a small typed transport and developer experience layer. It does not implement update installation, platform normalization, metadata verification, caching, or business rules. For a desktop app's check-on-launch, prompt and download flow, see [`desktopupdate`](desktopupdate).

## Desktop apps

`desktopupdate` runs a desktop app's update flow. It checks 30 seconds after `Start`, then every 6 hours; `CheckNow` runs a check on demand. It offers each new version through the app's own dialogs and opens the download the user accepts, preferring installers over archives. A declined download isn't offered again in the background unless it's marked critical. Development builds (a version such as `dev`) skip checks entirely.

The app supplies the dialogs, so any toolkit plugs in. With Wails:

```go
type wailsUI struct{ ctx context.Context }

func (u wailsUI) Ask(title, msg string) bool {
	answer, err := runtime.MessageDialog(u.ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: title, Message: msg,
		Buttons: []string{"Yes", "No"}, DefaultButton: "Yes", CancelButton: "No",
	})
	return err == nil && answer == "Yes"
}
func (u wailsUI) Tell(title, msg string, failed bool) {
	kind := runtime.InfoDialog
	if failed {
		kind = runtime.ErrorDialog
	}
	_, _ = runtime.MessageDialog(u.ctx, runtime.MessageDialogOptions{Type: kind, Title: title, Message: msg})
}
func (u wailsUI) Open(url string) { runtime.BrowserOpenURL(u.ctx, url) }

// In OnStartup:
updates := desktopupdate.New(desktopupdate.Config{
	Client:      appregistry.NewClient(appregistry.Config{BaseURL: "https://registry.example.com"}),
	Check:       appregistry.CheckOptions{Owner: "admin", AppName: "my-app", Version: appVersion, Channel: "stable", Platform: goruntime.GOOS, Arch: goruntime.GOARCH},
	ProductName: "My App",
	UI:          wailsUI{ctx},
})
updates.Start(ctx)
// In a menu: func(*menu.CallbackData) { go updates.CheckNow(ctx) }
```

Windows and Linux Wails dialogs only offer Yes/No buttons, which is why the prompts are phrased as questions.

## Installation

```sh
go get github.com/DacPhu/DPAppRegistry/sdk/go/appregistry
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

	appregistry "github.com/DacPhu/DPAppRegistry/sdk/go/appregistry"
)

func main() {
	client := appregistry.NewClient(appregistry.Config{
		BaseURL: "https://api.example.com",
	})

	resp, err := client.CheckForUpdates(context.Background(), appregistry.CheckOptions{
		Owner:    "admin",
		AppName:  "test",
		Version:  "0.0.0.5",
		Channel:  "nightly",
		Platform: "darwin",
		Arch:     "arm64",
	})
	if err != nil {
		log.Fatal(err)
	}

	if resp.UpdateAvailable {
		if resp.UpdateURL != "" {
			fmt.Printf("Update is available: %s\n", resp.UpdateURL)
		}
		for _, packageURL := range resp.PackageURLs {
			fmt.Printf("%s update is available: %s\n", packageURL.Package, packageURL.URL)
		}
	}
}
```

## Configuration

```go
client := appregistry.NewClient(appregistry.Config{
	BaseURL: "https://api.example.com",
	EdgeURL: "https://cdn.example.com",
	HTTPClient: &http.Client{
		Timeout: 10 * time.Second,
	},
})
```

`BaseURL` is required. It points to the DPAppRegistry API.

`EdgeURL` is optional. When configured, the SDK tries a static edge JSON response before falling back to the API.

`HTTPClient` is optional. When omitted, the SDK creates an `http.Client` with a default timeout. Custom clients are useful for timeouts, proxies, custom transports, and connection pooling policies.

The client is safe for concurrent use.

## Update Checks

`CheckForUpdates` sends a context-aware request and returns a typed response:

```go
resp, err := client.CheckForUpdates(ctx, appregistry.CheckOptions{
	Owner:    "admin",
	AppName:  "test",
	Version:  "0.0.0.5",
	Channel:  "nightly",
	Platform: "darwin",
	Arch:     "arm64",
	DeviceID: "optional-device-id",
})
```

`DeviceID` is optional. When set, the SDK sends it as the `X-Device-ID` header.

`DownloadToken` is optional. It is only needed for a private app whose download mode is `strict`.

## Private Apps

A private app with `download_mode: unlisted` is served like a public one: knowing the owner and app name is enough, and no token is involved. Only `download_mode: strict` needs one — without it the app answers an update check exactly as it answers a check for an app that does not exist. Pass the download token of that app and channel:

```go
resp, err := client.CheckForUpdates(ctx, appregistry.CheckOptions{
	Owner:         "admin",
	AppName:       "internal-tool",
	Version:       "1.2.0.4",
	Channel:       "stable",
	Platform:      "darwin",
	Arch:          "arm64",
	DownloadToken: os.Getenv("DPAPPREGISTRY_DOWNLOAD_TOKEN"),
})
```

The token is scoped to one app and one channel, and the SDK sends it as the `X-Download-Token` header. A token of another channel is refused like no token at all. `EdgeURL` is skipped whenever a token is set: DPAppRegistry refuses `cdn_edge` for private apps, so the edge lookup could only miss.

The SDK does not download artifacts. When the application fetches the returned URL itself, it sends the same header — and must drop it when DPAppRegistry redirects to storage:

```go
client := &http.Client{CheckRedirect: appregistry.StripDownloadTokenOnRedirect}

req, _ := http.NewRequest(http.MethodGet, resp.UpdateURL, nil)
req.Header.Set(appregistry.DownloadTokenHeader, token)
res, err := client.Do(req)
```

`/download` answers with a redirect to a presigned storage URL, and Go forwards custom headers across hosts: it only drops `Authorization`, `Cookie` and `WWW-Authenticate`. Without `StripDownloadTokenOnRedirect` the token ends up in the storage provider's access logs.

A runnable version of both steps is in `examples/private-app`.

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
bucket = sha256(deviceID + ":" + seed) → first 8 bytes, big-endian uint64, % 100
included if bucket < rollout.percent
```

When the install is **not** in the bucket, the SDK forces `UpdateAvailable` to `false` **and clears `UpdateURL`/`PackageURLs`** — so a caller that inspects the URLs instead of `UpdateAvailable` still cannot pull an update the device was not offered. The `Rollout` field on the response exposes the decision for logging:

```go
resp, err := client.CheckForUpdates(ctx, appregistry.CheckOptions{ /* ... */ DeviceID: "stable-device-id"})
if err != nil {
	log.Fatal(err)
}
if resp.Rollout != nil {
	fmt.Println(resp.Rollout.Percent, resp.Rollout.Bucket, resp.Rollout.Eligible)
}
```

`DeviceID` is required to participate: it must be the same stable value used for telemetry (`X-Device-ID`). Without it the bucket cannot be computed, so the install stays out of the rollout (`Eligible: false`, `Bucket: nil`) until a `DeviceID` is provided. Raising the percentage on the same version only ever adds installs. Rollout works identically in edge/CDN mode, since the same JSON body is served from the cached manifest.

The `appregistry.RolloutBucket(deviceID, seed)` helper is exported if you need to compute a bucket yourself.

## Base API Request

The BaseURL API request uses `GET /checkVersion`:

```text
GET /checkVersion?app_name=test&version=0.0.0.5&channel=nightly&platform=darwin&arch=arm64&owner=admin
X-Device-ID: optional
```

Query parameters are built from `CheckOptions` with typed fields. The SDK does not use untyped maps in its public API.

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

When a version is under a staged rollout, the response also carries a `rollout` object (`{ percent, seed }`), decoded into `resp.Rollout` — see [Staged Rollout](#staged-rollout).

The SDK decodes these into a typed response:

```go
if resp.UpdateURL != "" {
	fmt.Println(resp.UpdateURL)
}

for _, packageURL := range resp.PackageURLs {
	fmt.Println(packageURL.Package, packageURL.URL)
}
```

## EdgeURL Fallback

When `EdgeURL` is configured, the SDK first tries a static JSON response:

```text
GET /responses/{owner}/{app_name}/{channel}/{platform}/{arch}/manual/{version}.json
```

For example:

```text
GET /responses/admin/test/nightly/darwin/arm64/manual/0.0.0.5.json
```

If the edge response succeeds with HTTP 200 and valid JSON, `UpdateResponse.Source` is `SourceEdge`.

The SDK falls back to the BaseURL API when the edge request has:

- a network error;
- a timeout;
- invalid JSON;
- HTTP 404;
- any other non-200 response.

If the fallback API succeeds, `UpdateResponse.Source` is `SourceAPI`.

## Platform, Channel, And Architecture Values

DPAppRegistry supports fully custom platform, channel, and architecture values. This SDK never normalizes or remaps them.

The SDK will not change values such as:

- `macos` to `darwin`;
- `osx` to `darwin`;
- `stable` to `default`.

Whatever string you pass in `CheckOptions.Channel`, `CheckOptions.Platform`, and `CheckOptions.Arch` is the string sent to DPAppRegistry.

## Optional System Helpers

The SDK provides optional helpers:

```go
platform := appregistry.SystemPlatform() // runtime.GOOS
arch := appregistry.SystemArch()         // runtime.GOARCH
```

These helpers are never used automatically. Use them only when Go runtime values match your DPAppRegistry configuration.

## Error Handling

The SDK validates required fields and returns typed sentinel errors:

```go
resp, err := client.CheckForUpdates(ctx, opts)
if err != nil {
	switch {
	case errors.Is(err, appregistry.ErrMissingBaseURL):
		// Configure Config.BaseURL.
	case errors.Is(err, appregistry.ErrMissingOwner):
		// Set CheckOptions.Owner.
	case errors.Is(err, appregistry.ErrMissingAppName):
		// Set CheckOptions.AppName.
	case errors.Is(err, appregistry.ErrMissingVersion):
		// Set CheckOptions.Version.
	case errors.Is(err, appregistry.ErrRequestFailed):
		// Inspect the wrapped endpoint error.
	default:
		// Handle any other error.
	}
}
```

Request failures preserve underlying causes for `errors.Is` and `errors.As`:

```go
var endpointErr *appregistry.EndpointError
if errors.As(err, &endpointErr) {
	fmt.Println(endpointErr.URL)
	fmt.Println(endpointErr.StatusCode)
}
```

## Examples

Runnable examples are available in:

- `examples/basic`
- `examples/edge-fallback`
- `examples/custom-http-client`
- `examples/private-app`

## Security Scope

This SDK version performs update-check transport requests and typed response decoding only.

It does not verify TUF metadata, signatures, thresholds, expiration, rollback protection, or cache safety. Applications that need secure update metadata verification must perform that verification in the appropriate DPAppRegistry component or a future SDK layer that explicitly implements it.

No signature, threshold, expiration, rollback, freeze, root-of-trust, or cache protection is weakened by this transport-only SDK.
