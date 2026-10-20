package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// dailyFile appends to dir/<name>-YYYYMMDD.log, a new file each UTC day, and
// removes the files of all but the newest keep days. Nothing is ever renamed:
// the shipper reads each file to its end and picks up the next one by itself.
type dailyFile struct {
	dir, name string
	keep      int
	now       func() time.Time

	mu   sync.Mutex
	day  string
	file *os.File
}

// openDaily opens today's file, making dir if it is missing.
func openDaily(dir, name string, keep int) (*dailyFile, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	w := &dailyFile{dir: dir, name: name, keep: keep, now: time.Now}
	if err := w.open(w.now()); err != nil {
		return nil, err
	}
	return w, nil
}

// Write appends p to the day's file, moving to a new one at midnight UTC. It
// never fails: a file that can't be opened or written is given up until the
// next day, with a warning on stderr, where every line goes as well.
func (w *dailyFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if now.UTC().Format("20060102") != w.day {
		if err := w.open(now); err != nil {
			warn("Logging to stderr only until tomorrow: %v", err)
		}
	}
	if w.file != nil {
		if _, err := w.file.Write(p); err != nil {
			warn("Logging to stderr only until tomorrow: %v", err)
			w.file.Close()
			w.file = nil
		}
	}
	return len(p), nil
}

// open moves to now's file and removes the ones too old to keep. The day is
// taken first, so a file that can't be opened is tried again tomorrow, not at
// every line.
func (w *dailyFile) open(now time.Time) error {
	w.day = now.UTC().Format("20060102")
	if w.file != nil {
		w.file.Close()
		w.file = nil
	}
	file, err := os.OpenFile(filepath.Join(w.dir, w.name+"-"+w.day+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	// Readable by the shipper, which runs as another user, whatever the umask.
	_ = file.Chmod(0o644)
	w.file = file
	w.prune(now)
	return nil
}

func (w *dailyFile) prune(now time.Time) {
	oldest := now.UTC().AddDate(0, 0, 1-w.keep).Format("20060102")
	paths, _ := filepath.Glob(filepath.Join(w.dir, w.name+"-*.log"))
	for _, path := range paths {
		day := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), w.name+"-"), ".log")
		if _, err := time.Parse("20060102", day); err == nil && day < oldest {
			_ = os.Remove(path)
		}
	}
}

// warn writes a warning straight to stderr: Write runs inside logrus, under
// its lock.
func warn(format string, args ...any) {
	line, _ := Formatter{}.Format(&logrus.Entry{Time: time.Now(), Level: logrus.WarnLevel, Message: fmt.Sprintf(format, args...)})
	_, _ = os.Stderr.Write(line)
}
