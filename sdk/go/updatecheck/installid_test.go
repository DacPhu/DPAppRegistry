package updatecheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DacPhu/DPAppRegistry/sdk/go/appregistry"
)

// The tests run without install IDs, so they never write to the user's real config
// directory; TestNewGivesChecksAnInstallID turns them back on in a temporary one.
func TestMain(m *testing.M) {
	os.Setenv(appregistry.NoInstallIDEnv, "1")
	os.Exit(m.Run())
}

func TestNewGivesChecksAnInstallID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv(appregistry.NoInstallIDEnv, "")

	c := New(Config{Check: appregistry.CheckOptions{Owner: "acme", AppName: "tool"}})
	if len(c.cfg.Check.DeviceID) != 64 {
		t.Fatalf("DeviceID = %q, want a generated install id", c.cfg.Check.DeviceID)
	}
	if again := New(Config{Check: appregistry.CheckOptions{Owner: "acme", AppName: "tool"}}); again.cfg.Check.DeviceID != c.cfg.Check.DeviceID {
		t.Fatalf("the install id changed between runs")
	}
	given := New(Config{Check: appregistry.CheckOptions{Owner: "acme", AppName: "tool", DeviceID: "from-the-app"}})
	if given.cfg.Check.DeviceID != "from-the-app" {
		t.Fatalf("an app's own DeviceID must win, got %q", given.cfg.Check.DeviceID)
	}
}
