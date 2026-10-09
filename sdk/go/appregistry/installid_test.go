package appregistry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallIDIsMadeOnceAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "install-id")
	first, err := installIDAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if !installIDPattern.MatchString(first) {
		t.Fatalf("install id %q is not 64 hex digits", first)
	}
	again, err := installIDAt(path)
	if err != nil || again != first {
		t.Fatalf("second read = %q, %v; want %q", again, err, first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("install id file mode = %v, want it private to the user", mode)
	}
}

func TestInstallIDReplacesAnUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install-id")
	if err := os.WriteFile(path, []byte("not an id"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := installIDAt(path)
	if err != nil || !installIDPattern.MatchString(id) {
		t.Fatalf("install id = %q, %v", id, err)
	}
}

func TestInstallIDDiffersPerAppAndCanBeTurnedOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	editor, err := InstallID("acme", "editor")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := InstallID("acme", "cli")
	if err != nil {
		t.Fatal(err)
	}
	if editor == "" || editor == cli {
		t.Fatalf("editor %q and cli %q should be two different ids", editor, cli)
	}
	if again, _ := InstallID("acme", "editor"); again != editor {
		t.Fatalf("editor's id changed from %q to %q", editor, again)
	}

	t.Setenv(NoInstallIDEnv, "1")
	if off, err := InstallID("acme", "editor"); off != "" || err != nil {
		t.Fatalf("with %s set: %q, %v; want nothing", NoInstallIDEnv, off, err)
	}
}
