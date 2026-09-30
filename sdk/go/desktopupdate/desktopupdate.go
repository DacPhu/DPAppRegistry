// Package desktopupdate runs a desktop app's update checks against
// DPAppRegistry: shortly after launch, then on an interval, and on demand. It
// offers each new version through the app's own dialogs and opens the
// download the user accepts. The app supplies those dialogs (UI), so any
// toolkit plugs in: Wails, Fyne, a terminal.
//
// It opens a download rather than installing in place, because installers
// differ per platform and toolkit. Electron apps should use the JS SDK's
// electron helper instead, which hands electron-builder feeds to
// electron-updater.
package desktopupdate

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/DacPhu/DPAppRegistry/sdk/go/appregistry"
)

// UI is what the checker needs from the app's toolkit.
type UI interface {
	// Ask poses a yes/no question and reports whether the user said yes.
	Ask(title, message string) bool
	// Tell shows a message; failed marks an error.
	Tell(title, message string, failed bool)
	// Open opens a download URL, usually in the browser.
	Open(url string)
}

// Config describes one app. Check identifies it the way DPAppRegistry does
// (Owner, AppName, Channel, Platform, Arch) and carries the running Version.
type Config struct {
	Client      *appregistry.Client
	Check       appregistry.CheckOptions
	ProductName string // shown in the dialogs
	UI          UI

	FirstCheck time.Duration // after Start; default 30s
	Interval   time.Duration // between background checks; default 6h

	// Packages ranks download formats, most preferred first. Names are
	// DPAppRegistry package names: file extensions without the dot. Default:
	// DefaultPackages for Check.Platform.
	Packages []string

	Logf func(format string, args ...any) // optional
}

// DefaultPackages ranks download formats for a platform: installers before
// archives.
func DefaultPackages(platform string) []string {
	switch platform {
	case "darwin":
		return []string{"dmg", "pkg", "zip"}
	case "windows":
		return []string{"exe", "msi", "zip"}
	}
	return []string{"AppImage", "deb", "rpm", "gz", "zip"}
}

// release matches the versions DPAppRegistry accepts: numbers joined by dots
// or dashes. A development build such as "dev" has nothing to compare.
var release = regexp.MustCompile(`^[0-9]+([.-][0-9]+)*$`)

// IsRelease reports whether version can be checked for updates.
func IsRelease(version string) bool { return release.MatchString(version) }

// Checker runs checks for one app. Use New.
type Checker struct {
	cfg       Config
	mu        sync.Mutex // one check at a time
	dismissed string     // download the user declined; not offered again unasked
}

// New returns a Checker, filling in Config defaults.
func New(cfg Config) *Checker {
	if cfg.FirstCheck <= 0 {
		cfg.FirstCheck = 30 * time.Second
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 6 * time.Hour
	}
	if len(cfg.Packages) == 0 {
		cfg.Packages = DefaultPackages(cfg.Check.Platform)
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Checker{cfg: cfg}
}

// Start runs background checks until ctx ends: FirstCheck after the call, then
// every Interval. It returns at once, and does nothing for a development build.
func (c *Checker) Start(ctx context.Context) {
	if !IsRelease(c.cfg.Check.Version) {
		c.cfg.Logf("update checks off: %q is not a release version", c.cfg.Check.Version)
		return
	}
	go func() {
		timer := time.NewTimer(c.cfg.FirstCheck)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				c.check(ctx, false)
				timer.Reset(c.cfg.Interval)
			}
		}
	}()
}

// CheckNow checks because the user asked (a "Check for Updates…" item): unlike
// a background check it also reports "up to date" and failures, and offers a
// download the user declined before.
func (c *Checker) CheckNow(ctx context.Context) { c.check(ctx, true) }

func (c *Checker) check(ctx context.Context, interactive bool) {
	if !c.mu.TryLock() {
		return // a check is already running
	}
	defer c.mu.Unlock()

	name, version, ui := c.cfg.ProductName, c.cfg.Check.Version, c.cfg.UI
	if !IsRelease(version) {
		if interactive {
			ui.Tell("Development build", "Version "+version+" is not a release, so there is nothing to compare against.", false)
		}
		return
	}
	resp, err := c.cfg.Client.CheckForUpdates(ctx, c.cfg.Check)
	if err != nil {
		c.cfg.Logf("update check: %v", err)
		if interactive {
			ui.Tell("Could not check for updates", err.Error(), true)
		}
		return
	}
	if !resp.UpdateAvailable {
		if interactive {
			ui.Tell(name+" is up to date", "Version "+version+" is the newest available.", false)
		}
		return
	}
	url := c.download(resp)
	if url == "" {
		c.cfg.Logf("update available, but no download for %s/%s", c.cfg.Check.Platform, c.cfg.Check.Arch)
		if interactive {
			ui.Tell("Update not available here", "A new version of "+name+" exists, but not as a download for this system.", true)
		}
		return
	}
	// A critical update is offered on every check, even after a "no".
	if !interactive && !resp.Critical && url == c.dismissed {
		return
	}
	msg := "You have " + version + ". Download the new version now?"
	if resp.Critical {
		msg = "This update is marked critical. " + msg
	}
	if notes := strings.TrimSpace(resp.Changelog); notes != "" {
		msg += "\n\n" + notes
	}
	if !ui.Ask("A new version of "+name+" is available", msg) {
		c.dismissed = url
		return
	}
	ui.Open(url)
}

// download picks this platform's download from a response: the first format
// in Packages, else a package-less update_url.
func (c *Checker) download(resp *appregistry.UpdateResponse) string {
	for _, want := range c.cfg.Packages {
		for _, p := range resp.PackageURLs {
			if strings.EqualFold(p.Package, want) && p.URL != "" {
				return p.URL
			}
		}
	}
	return resp.UpdateURL
}
