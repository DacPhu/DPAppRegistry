package desktopupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DacPhu/DPAppRegistry/sdk/go/appregistry"
)

// fakeUI records what the checker showed and answers Ask with yes.
type fakeUI struct {
	yes    bool
	asked  []string
	told   []string
	opened []string
}

func (u *fakeUI) Ask(title, _ string) bool          { u.asked = append(u.asked, title); return u.yes }
func (u *fakeUI) Tell(title, _ string, failed bool) { u.told = append(u.told, title) }
func (u *fakeUI) Open(url string)                   { u.opened = append(u.opened, url) }

// registry serves a fixed checkVersion body and counts requests.
func registry(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/checkVersion" || r.URL.Query().Get("app_name") != "tool" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func checker(srv *httptest.Server, version string, ui UI) *Checker {
	return New(Config{
		Client:      appregistry.NewClient(appregistry.Config{BaseURL: srv.URL, HTTPClient: srv.Client()}),
		Check:       appregistry.CheckOptions{Owner: "admin", AppName: "tool", Version: version, Channel: "stable", Platform: "linux", Arch: "amd64"},
		ProductName: "Tool",
		UI:          ui,
	})
}

const available = `{"update_available":true,"update_url_zip":"https://r/tool.zip","update_url_AppImage":"https://r/tool.AppImage"}`

func TestUpdateOpensThePreferredDownload(t *testing.T) {
	srv, _ := registry(t, http.StatusOK, available)
	ui := &fakeUI{yes: true}
	checker(srv, "1.2.0", ui).CheckNow(context.Background())
	if len(ui.asked) != 1 || len(ui.opened) != 1 || ui.opened[0] != "https://r/tool.AppImage" {
		t.Fatalf("asked %v, opened %v; want one offer opening the AppImage (preferred on linux)", ui.asked, ui.opened)
	}
}

func TestDeclinedUpdateIsNotOfferedAgainUnasked(t *testing.T) {
	srv, _ := registry(t, http.StatusOK, available)
	ui := &fakeUI{}
	c := checker(srv, "1.2.0", ui)
	c.check(context.Background(), false)
	c.check(context.Background(), false)
	if len(ui.asked) != 1 {
		t.Fatalf("background checks asked %d times, want once", len(ui.asked))
	}
	c.CheckNow(context.Background())
	if len(ui.asked) != 2 {
		t.Errorf("an explicit check did not offer the declined update again")
	}
}

func TestCriticalUpdateIsOfferedEvenAfterNo(t *testing.T) {
	srv, _ := registry(t, http.StatusOK, `{"update_available":true,"critical":true,"update_url_zip":"https://r/tool.zip"}`)
	ui := &fakeUI{}
	c := checker(srv, "1.2.0", ui)
	c.check(context.Background(), false)
	c.check(context.Background(), false)
	if len(ui.asked) != 2 {
		t.Errorf("critical update asked %d times over two checks, want 2", len(ui.asked))
	}
}

func TestBackgroundCheckStaysQuietUnlessThereIsNews(t *testing.T) {
	upToDate, _ := registry(t, http.StatusOK, `{"update_available":false}`)
	ui := &fakeUI{}
	checker(upToDate, "1.2.0", ui).check(context.Background(), false)
	broken, _ := registry(t, http.StatusInternalServerError, `oops`)
	checker(broken, "1.2.0", ui).check(context.Background(), false)
	if len(ui.told)+len(ui.asked) != 0 {
		t.Errorf("background checks showed %v / %v; want nothing", ui.told, ui.asked)
	}
}

func TestExplicitCheckReportsUpToDateAndFailures(t *testing.T) {
	upToDate, _ := registry(t, http.StatusOK, `{"update_available":false}`)
	broken, _ := registry(t, http.StatusInternalServerError, `oops`)
	ui := &fakeUI{}
	checker(upToDate, "1.2.0", ui).CheckNow(context.Background())
	checker(broken, "1.2.0", ui).CheckNow(context.Background())
	if len(ui.told) != 2 || !strings.Contains(ui.told[0], "up to date") || !strings.Contains(ui.told[1], "Could not check") {
		t.Errorf("told %v; want an up-to-date then a failure message", ui.told)
	}
}

func TestDevelopmentBuildNeverAsksTheServer(t *testing.T) {
	srv, hits := registry(t, http.StatusOK, available)
	ui := &fakeUI{}
	c := checker(srv, "dev", ui)
	c.Start(context.Background())
	c.CheckNow(context.Background())
	if hits.Load() != 0 || len(ui.told) != 1 || ui.told[0] != "Development build" {
		t.Errorf("hits %d, told %v; want no request and one development-build note", hits.Load(), ui.told)
	}
}

func TestStartChecksAfterFirstCheckUntilCancelled(t *testing.T) {
	srv, hits := registry(t, http.StatusOK, `{"update_available":false}`)
	c := checker(srv, "1.2.0", &fakeUI{})
	c.cfg.FirstCheck, c.cfg.Interval = 10*time.Millisecond, 10*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if hits.Load() < 2 {
		t.Fatalf("got %d background checks, want at least 2", hits.Load())
	}
	time.Sleep(30 * time.Millisecond) // let a check in flight finish
	after := hits.Load()
	time.Sleep(50 * time.Millisecond)
	if hits.Load() != after {
		t.Error("checks continued after the context ended")
	}
}

func TestDefaultPackagesPreferInstallers(t *testing.T) {
	for platform, first := range map[string]string{"darwin": "dmg", "windows": "exe", "linux": "AppImage"} {
		if got := DefaultPackages(platform)[0]; got != first {
			t.Errorf("DefaultPackages(%s)[0] = %s, want %s", platform, got, first)
		}
	}
}
