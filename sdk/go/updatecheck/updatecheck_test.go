package updatecheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DacPhu/DPAppRegistry/sdk/go/appregistry"
)

// fakeRegistry serves a checkVersion body that a test can change, and counts
// requests.
type fakeRegistry struct {
	*httptest.Server
	hits atomic.Int32

	mu     sync.Mutex
	status int
	body   string
}

func (f *fakeRegistry) answer(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.body = status, body
}

func registry(t *testing.T, body string) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{status: http.StatusOK, body: body}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.URL.Path != "/checkVersion" || r.URL.Query().Get("app_name") != "tool" {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		status, body := f.status, f.body
		f.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func checker(f *fakeRegistry, version string, cfg Config) *Checker {
	cfg.Client = appregistry.NewClient(appregistry.Config{BaseURL: f.URL, HTTPClient: f.Client()})
	cfg.Check = appregistry.CheckOptions{Owner: "admin", AppName: "tool", Version: version, Channel: "stable", Platform: "darwin", Arch: "arm64"}
	return New(cfg)
}

const (
	available = `{"update_available":true,"version":"1.3.0","changelog":"Faster copies.\n","update_url_gz":"https://r/tool.tar.gz","update_url_zip":"https://r/tool.zip"}`
	critical  = `{"update_available":true,"critical":true,"update_url_gz":"https://r/tool.tar.gz"}`
	upToDate  = `{"update_available":false}`
)

func TestCheckFindsTheNewerRelease(t *testing.T) {
	f := registry(t, available)
	c := checker(f, "1.2.0", Config{Packages: []string{"zip", "gz"}})
	u, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Update{Version: "1.3.0", URL: "https://r/tool.zip", Changelog: "Faster copies."}
	if u == nil || *u != want {
		t.Fatalf("Check = %+v, want %+v", u, want)
	}
	if got := c.Latest(); got == nil || *got != want {
		t.Errorf("Latest = %+v, want what Check found", got)
	}
}

func TestWithoutPackagesTheReleasesDownloadIsUsed(t *testing.T) {
	f := registry(t, `{"update_available":true,"update_url_gz":"https://r/tool.tar.gz"}`)
	u, err := checker(f, "1.2.0", Config{}).Check(context.Background())
	if err != nil || u == nil || u.URL != "https://r/tool.tar.gz" {
		t.Fatalf("Check = %+v, %v; want the release's only download", u, err)
	}
}

func TestUpToDateClearsLatestAndAFailureKeepsIt(t *testing.T) {
	f := registry(t, available)
	c := checker(f, "1.2.0", Config{})
	if _, err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}

	f.answer(http.StatusInternalServerError, `{"error":"down"}`)
	if _, err := c.Check(context.Background()); err == nil {
		t.Fatal("a failed check returned no error")
	}
	if c.Latest() == nil {
		t.Fatal("a failed check cleared what the last one found")
	}

	f.answer(http.StatusOK, upToDate)
	u, err := c.Check(context.Background())
	if err != nil || u != nil || c.Latest() != nil {
		t.Fatalf("up to date: Check = %+v, %v, Latest = %+v; want nil everywhere", u, err, c.Latest())
	}
}

func TestDevelopmentBuildNeverAsks(t *testing.T) {
	f := registry(t, available)
	c := checker(f, "dev", Config{FirstCheck: time.Millisecond})
	if _, err := c.Check(context.Background()); !errors.Is(err, ErrNotRelease) {
		t.Fatalf("Check on a dev build = %v, want ErrNotRelease", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	if n := f.hits.Load(); n != 0 {
		t.Fatalf("a dev build asked the registry %d times", n)
	}
}

func TestBackgroundChecksReportEachDownloadOnce(t *testing.T) {
	f := registry(t, available)
	var reports []Update
	c := checker(f, "1.2.0", Config{OnUpdate: func(u Update) { reports = append(reports, u) }})
	c.background(context.Background())
	c.background(context.Background())
	if len(reports) != 1 {
		t.Fatalf("reported %d times, want once", len(reports))
	}

	// Up to date in between, then released again: that is news again.
	f.answer(http.StatusOK, upToDate)
	c.background(context.Background())
	f.answer(http.StatusOK, available)
	c.background(context.Background())
	if len(reports) != 2 {
		t.Fatalf("reported %d times, want the re-release reported too", len(reports))
	}
}

func TestCriticalUpdateIsReportedOnEveryCheck(t *testing.T) {
	f := registry(t, critical)
	var reports int
	c := checker(f, "1.2.0", Config{OnUpdate: func(u Update) {
		if u.Critical {
			reports++
		}
	}})
	c.background(context.Background())
	c.background(context.Background())
	if reports != 2 {
		t.Fatalf("critical update reported %d times over 2 checks, want 2", reports)
	}
}

func TestStartChecksInTheBackground(t *testing.T) {
	f := registry(t, available)
	found := make(chan Update, 1)
	c := checker(f, "1.2.0", Config{FirstCheck: time.Millisecond, OnUpdate: func(u Update) { found <- u }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	select {
	case u := <-found:
		if u.URL == "" {
			t.Errorf("reported %+v without a download", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no background check reported the update")
	}
}
