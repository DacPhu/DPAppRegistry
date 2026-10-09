package desktopupdate

import (
	"os"
	"testing"

	"github.com/DacPhu/DPAppRegistry/sdk/go/appregistry"
)

// The tests run without install IDs, so they never write to the user's real config
// directory; updatecheck's tests cover how New fills one in.
func TestMain(m *testing.M) {
	os.Setenv(appregistry.NoInstallIDEnv, "1")
	os.Exit(m.Run())
}
