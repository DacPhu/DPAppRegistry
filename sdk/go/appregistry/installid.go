package appregistry

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// NoInstallIDEnv turns install IDs off when set to any value: checks then go out
// without X-Device-ID, and the install stays out of staged rollouts.
const NoInstallIDEnv = "DPAPPREGISTRY_NO_INSTALL_ID"

var (
	installIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	unsafeName       = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

// InstallID returns the ID this install of owner's app sends as X-Device-ID, made
// on first use and kept in the user's config directory. It is 32 random bytes, so it
// tells installs apart without saying anything about the person or the machine;
// deleting the file makes a new one. DPAppRegistry counts an app's monthly active
// installs by it, and staged rollouts pick installs by it.
//
// It returns "" with no error when NoInstallIDEnv is set.
func InstallID(owner, app string) (string, error) {
	if os.Getenv(NoInstallIDEnv) != "" {
		return "", nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	name := unsafeName.ReplaceAllString(owner+"-"+app, "_")
	return installIDAt(filepath.Join(dir, "dpappregistry", name, "install-id"))
}

func installIDAt(path string) (string, error) {
	if id, ok := readInstallID(path); ok {
		return id, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random)

	// Written aside and renamed into place, so a reader never sees half an ID.
	tmp, err := os.CreateTemp(filepath.Dir(path), "install-id-*")
	if err != nil {
		return "", err
	}
	_, writeErr := tmp.WriteString(id + "\n")
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	// When another process got there first, its ID wins and both use it.
	if existing, ok := readInstallID(path); ok {
		os.Remove(tmp.Name())
		return existing, nil
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return id, nil
}

func readInstallID(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(raw))
	return id, installIDPattern.MatchString(id)
}
