// Package updatecheck watches DPAppRegistry for a newer release of a program
// with no window to ask in: a background service or a command-line tool. It
// checks in the background and on demand, and keeps what it found; the program
// decides how to tell its user, whether in a log line, a notice in its web
// console or a message on the terminal.
//
// It only reports. Downloading and installing stay with the program, since
// replacing a running service depends on how it was installed. Desktop apps
// that can ask in a dialog use desktopupdate instead.
package updatecheck

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/DacPhu/DPAppRegistry/sdk/go/appregistry"
)

// Config describes one program. Check identifies it the way DPAppRegistry does
// (Owner, AppName, Channel, Platform, Arch) and carries the running Version.
type Config struct {
	Client *appregistry.Client
	Check  appregistry.CheckOptions

	FirstCheck time.Duration // after Start; default 30s
	Interval   time.Duration // between background checks; default 6h

	// Packages ranks download formats, most preferred first. Names are
	// DPAppRegistry package names: the file extension without the dot, so "gz"
	// for a .tar.gz. Default: whichever package the release has.
	Packages []string

	// OnUpdate is called when a background check finds an update it has not
	// reported yet: a new download, or a critical one, which is reported on
	// every check. Optional.
	OnUpdate func(Update)

	Logf func(format string, args ...any) // optional
}

// Update is a newer release than the running one.
type Update struct {
	// URL is the download for this platform, in the most preferred format. It
	// is empty when the release has no download for this platform.
	URL       string
	Critical  bool
	Changelog string
}

// ErrNotRelease is what Check returns for a development build, whose version
// (such as "dev" or a commit hash) has nothing to compare against.
var ErrNotRelease = errors.New("not a release version")

// Checker runs checks for one program. Use New.
type Checker struct {
	cfg      Config
	checking sync.Mutex // one request at a time

	mu       sync.Mutex
	latest   *Update // what the last successful check found; nil when up to date
	reported *Update // what OnUpdate was last called with
}

// New returns a Checker, filling in Config defaults.
func New(cfg Config) *Checker {
	if cfg.FirstCheck <= 0 {
		cfg.FirstCheck = 30 * time.Second
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 6 * time.Hour
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Checker{cfg: cfg}
}

// Start runs background checks until ctx ends: FirstCheck after the call, then
// every Interval. It returns at once, and does nothing for a development build.
func (c *Checker) Start(ctx context.Context) {
	if !appregistry.IsRelease(c.cfg.Check.Version) {
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
				c.background(ctx)
				timer.Reset(c.cfg.Interval)
			}
		}
	}()
}

func (c *Checker) background(ctx context.Context) {
	u, err := c.Check(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.cfg.Logf("update check: %v", err)
		}
		return
	}
	c.mu.Lock()
	fresh := u != nil && (c.reported == nil || u.Critical || u.URL != c.reported.URL)
	c.reported = u
	c.mu.Unlock()
	if fresh && c.cfg.OnUpdate != nil {
		c.cfg.OnUpdate(*u)
	}
}

// Check asks DPAppRegistry now. It returns the newer release, or nil when this
// is the newest; Latest returns the same afterwards. A failed check leaves
// Latest as it was.
func (c *Checker) Check(ctx context.Context) (*Update, error) {
	if !appregistry.IsRelease(c.cfg.Check.Version) {
		return nil, ErrNotRelease
	}
	c.checking.Lock()
	defer c.checking.Unlock()

	resp, err := c.cfg.Client.CheckForUpdates(ctx, c.cfg.Check)
	if err != nil {
		return nil, err
	}
	var u *Update
	if resp.UpdateAvailable {
		u = &Update{URL: c.download(resp), Critical: resp.Critical, Changelog: strings.TrimSpace(resp.Changelog)}
	}
	c.mu.Lock()
	c.latest = u
	c.mu.Unlock()
	return copyOf(u), nil
}

// Latest returns what the last successful check found: the newer release, or
// nil when there is none or no check has succeeded yet.
func (c *Checker) Latest() *Update {
	c.mu.Lock()
	defer c.mu.Unlock()
	return copyOf(c.latest)
}

func copyOf(u *Update) *Update {
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}

// download picks this platform's download: the first format in Packages; with
// no Packages, the first package the release has; else a package-less
// update_url.
func (c *Checker) download(resp *appregistry.UpdateResponse) string {
	for _, want := range c.cfg.Packages {
		for _, p := range resp.PackageURLs {
			if strings.EqualFold(p.Package, want) && p.URL != "" {
				return p.URL
			}
		}
	}
	if len(c.cfg.Packages) == 0 {
		for _, p := range resp.PackageURLs {
			if p.URL != "" {
				return p.URL
			}
		}
	}
	return resp.UpdateURL
}
