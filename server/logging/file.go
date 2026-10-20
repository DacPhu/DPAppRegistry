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

// How long a log file that couldn't be opened or written is left alone before
// the next try. Every line goes to stderr meanwhile.
const retryEvery = time.Minute

// dailyFile appends to dir/<name>-YYYYMMDD.log, a new file each UTC day, and
// removes the files of all but the newest keep days. Nothing is ever renamed:
// the shipper reads each file to its end and picks up the next one by itself.
type dailyFile struct {
	dir, name string
	keep      int
	now       func() time.Time

	mu      sync.Mutex
	day     string
	file    *os.File
	retryAt time.Time // no try to open a file before then
	failing bool      // no line has reached a file since the last warning
}

func newDailyFile(dir, name string, keep int) *dailyFile {
	return &dailyFile{dir: dir, name: name, keep: keep, now: time.Now}
}

// start opens today's file. One it can't open is tried again by Write; the
// caller warns about this first failure.
func (w *dailyFile) start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if err := w.open(now); err != nil {
		w.retryAt = now.Add(retryEvery)
		w.failing = true
		return err
	}
	return nil
}

// Write appends p to the day's file, moving to a new one at midnight UTC. It
// never fails, since every line goes to stderr too: a file that can't be
// opened or written is tried again at most once a minute, with one warning
// per outage and a note once a line reaches a file again.
func (w *dailyFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if w.file != nil && now.UTC().Format("20060102") != w.day {
		w.close()
	}
	if w.file == nil {
		if now.Before(w.retryAt) {
			return len(p), nil
		}
		if err := w.open(now); err != nil {
			w.fail(now, err)
			return len(p), nil
		}
	}
	if _, err := w.file.Write(p); err != nil {
		w.close()
		w.fail(now, err)
		return len(p), nil
	}
	if w.failing {
		w.failing = false
		note(logrus.InfoLevel, "Writing the log file in %s again", w.dir)
	}
	return len(p), nil
}

// fail leaves the file alone for retryEvery, warning only at an outage's
// first failure.
func (w *dailyFile) fail(now time.Time, err error) {
	w.retryAt = now.Add(retryEvery)
	if !w.failing {
		w.failing = true
		note(logrus.WarnLevel, "Logging to stderr only, trying the log file again every minute: %v", err)
	}
}

func (w *dailyFile) close() {
	_ = w.file.Close()
	w.file = nil
}

// open moves to now's file, making its folder if it is missing, and removes
// the files too old to keep.
func (w *dailyFile) open(now time.Time) error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	day := now.UTC().Format("20060102")
	file, err := os.OpenFile(filepath.Join(w.dir, w.name+"-"+day+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	// Readable by the shipper, which runs as another user, whatever the umask.
	_ = file.Chmod(0o644)
	w.file, w.day = file, day
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

// note writes a line straight to stderr: Write runs inside logrus, under its
// lock.
func note(level logrus.Level, format string, args ...any) {
	line, _ := Formatter{}.Format(&logrus.Entry{Time: time.Now(), Level: level, Message: fmt.Sprintf(format, args...)})
	_, _ = stderr.Write(line)
}
