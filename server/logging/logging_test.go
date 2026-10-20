package logging

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The infra repo's Alloy (alloy/roles/alloy/templates/config.alloy.j2): a line
// that starts an entry, the time and level it reads from it, and the file
// name it takes the file and service labels from.
var (
	alloyFirstLine = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z `)
	alloyLevel     = regexp.MustCompile(`^(?P<time>\S+) (?P<level>[A-Z]+) `)
	alloyFile      = regexp.MustCompile(`/(?P<file>(?P<service>[a-z]+)-\d+\.log)$`)
)

// Seven in the evening in Vietnam, noon UTC.
var evening = time.Date(2026, 10, 20, 19, 0, 0, 987654321, time.FixedZone("ICT", 7*60*60))

func format(t *testing.T, level logrus.Level, message string, fields logrus.Fields) string {
	t.Helper()
	line, err := Formatter{}.Format(&logrus.Entry{Time: evening, Level: level, Message: message, Data: fields})
	require.NoError(t, err)
	return string(line)
}

// captureLogs points the standard logger at a buffer, in this package's format,
// until the test ends.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	logger := logrus.StandardLogger()
	out, formatter, level := logger.Out, logger.Formatter, logger.GetLevel()
	t.Cleanup(func() {
		logger.SetOutput(out)
		logger.SetFormatter(formatter)
		logger.SetLevel(level)
	})
	var buffer bytes.Buffer
	logger.SetOutput(&buffer)
	logger.SetFormatter(Formatter{})
	logger.SetLevel(logrus.InfoLevel)
	return &buffer
}

func TestFormatterLineMatchesTheShipper(t *testing.T) {
	line := format(t, logrus.InfoLevel, "Telemetry allow-list reloaded after creating app\n", logrus.Fields{})
	assert.Equal(t, "2026-10-20T12:00:00Z INFO api Telemetry allow-list reloaded after creating app\n", line)

	assert.Regexp(t, alloyFirstLine, line)
	match := alloyLevel.FindStringSubmatch(line)
	require.NotNil(t, match)
	when, err := time.Parse(time.RFC3339, match[alloyLevel.SubexpIndex("time")])
	require.NoError(t, err)
	assert.True(t, when.Equal(evening.Truncate(time.Second)), "%s is not %s", when, evening)
	assert.Equal(t, "INFO", match[alloyLevel.SubexpIndex("level")])
}

func TestFormatterLevels(t *testing.T) {
	for level, want := range map[logrus.Level]string{
		logrus.TraceLevel: "DEBUG",
		logrus.DebugLevel: "DEBUG",
		logrus.InfoLevel:  "INFO",
		logrus.WarnLevel:  "WARNING",
		logrus.ErrorLevel: "ERROR",
		logrus.FatalLevel: "CRITICAL",
		logrus.PanicLevel: "CRITICAL",
	} {
		match := alloyLevel.FindStringSubmatch(format(t, level, "something", nil))
		require.NotNil(t, match, level.String())
		assert.Equal(t, want, match[alloyLevel.SubexpIndex("level")], level.String())
	}
}

func TestFormatterFields(t *testing.T) {
	line := format(t, logrus.ErrorLevel, "Failed to reload telemetry allow-list", logrus.Fields{
		"error":       errors.New("connection refused"),
		"transaction": 42,
		"empty":       "",
		"App Name":    "Desk App",
	})
	assert.Equal(t, `2026-10-20T12:00:00Z ERROR api Failed to reload telemetry allow-list "App Name"="Desk App" empty="" error="connection refused" transaction=42`+"\n", line)

	// The logger field names the logger instead of being a field.
	assert.Equal(t, "2026-10-20T12:00:00Z INFO http trace=- method=GET\n", format(t, logrus.InfoLevel, "trace=- method=GET", logrus.Fields{loggerField: "http"}))
}

// A client's text with a newline and a whole forged entry in it, in a message
// or a field, never starts an entry of its own.
func TestFormatterKeepsAClientsNewlinesInsideTheEntry(t *testing.T) {
	forged := "x\n2026-10-20T12:00:00Z CRITICAL api forged"

	line := format(t, logrus.WarnLevel, "No results found for parameters: map[app_name:"+forged+"]", nil)
	lines := strings.Split(strings.TrimSuffix(line, "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Regexp(t, alloyFirstLine, lines[0])
	assert.Equal(t, "  2026-10-20T12:00:00Z CRITICAL api forged]", lines[1])
	assert.NotRegexp(t, alloyFirstLine, lines[1])

	line = format(t, logrus.ErrorLevel, "Upload failed", logrus.Fields{"error": forged, "name": "a=b c"})
	assert.Equal(t, 1, strings.Count(line, "\n"), line)
	assert.Contains(t, line, `error="x\n2026-10-20T12:00:00Z CRITICAL api forged" name="a=b c"`)
}

// Every other control character in a message is escaped: a carriage return
// could overwrite the line in a terminal following the log, and ESC repaint it.
func TestFormatterEscapesControlCharacters(t *testing.T) {
	line := format(t, logrus.WarnLevel, "a\rb\tc\x1b[2Kd\x7fe\u0085f\x00g\n\r2026-10-20T12:00:00Z CRITICAL api forged", nil)
	assert.Equal(t, "2026-10-20T12:00:00Z WARNING api a\\rb\\tc\\x1b[2Kd\\x7fe\\u0085f\\x00g\n  \\r2026-10-20T12:00:00Z CRITICAL api forged\n", line)
	for _, r := range strings.TrimSuffix(line, "\n") {
		if r != '\n' {
			assert.False(t, r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0), "control character %q left in %q", r, line)
		}
	}
}

func TestDailyFileNamesRotatesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	// A file 19 days old and one 13 days old, and files that aren't the API's daily ones.
	for _, name := range []string{"api-20261001.log", "api-20261007.log", "api-latest.log", "worker-20261001.log", "api-20261001.log.1"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("old\n"), 0o644))
	}
	now := evening
	w := &dailyFile{dir: dir, name: "api", keep: 14, now: func() time.Time { return now }}

	_, err := w.Write([]byte("first\n"))
	require.NoError(t, err)
	_, err = w.Write([]byte("second\n"))
	require.NoError(t, err)
	today := filepath.Join(dir, "api-20261020.log")
	assert.FileExists(t, today)
	content, err := os.ReadFile(today)
	require.NoError(t, err)
	assert.Equal(t, "first\nsecond\n", string(content))
	info, err := os.Stat(today)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())

	match := alloyFile.FindStringSubmatch(today)
	require.NotNil(t, match, today)
	assert.Equal(t, "api-20261020.log", match[alloyFile.SubexpIndex("file")])
	assert.Equal(t, "api", match[alloyFile.SubexpIndex("service")])

	assert.NoFileExists(t, filepath.Join(dir, "api-20261001.log"), "older than 14 days")
	for _, kept := range []string{"api-20261007.log", "api-latest.log", "worker-20261001.log", "api-20261001.log.1"} {
		assert.FileExists(t, filepath.Join(dir, kept))
	}

	// Past midnight UTC, the next line goes to the next day's file.
	now = time.Date(2026, 10, 21, 0, 0, 1, 0, time.UTC)
	_, err = w.Write([]byte("third\n"))
	require.NoError(t, err)
	content, err = os.ReadFile(filepath.Join(dir, "api-20261021.log"))
	require.NoError(t, err)
	assert.Equal(t, "third\n", string(content))
	content, err = os.ReadFile(today)
	require.NoError(t, err)
	assert.Equal(t, "first\nsecond\n", string(content), "a day's file is never touched again")
	assert.NoFileExists(t, filepath.Join(dir, "api-20261007.log"), "now older than 14 days")
}

// captureStderr points stderr, where every line goes and the file's own
// warnings too, at a buffer until the test ends.
func captureStderr(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := stderr
	t.Cleanup(func() { stderr = previous })
	var buffer bytes.Buffer
	stderr = &buffer
	return &buffer
}

// A folder that can't be made (a file stands in its way): every line still
// reports written, for stderr's sake, and the file is tried again once a
// minute, with one warning for the whole outage.
func TestDailyFileRetriesAnOpenOnceAMinute(t *testing.T) {
	notes := captureStderr(t)
	base := t.TempDir()
	blocker := filepath.Join(base, "logs")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))
	now := evening
	w := &dailyFile{dir: blocker, name: "api", keep: 14, now: func() time.Time { return now }}

	for range 3 {
		n, err := w.Write([]byte("lost\n"))
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
	}
	assert.Equal(t, 1, strings.Count(notes.String(), " WARNING api Logging to stderr only, trying the log file again every minute: "), notes.String())

	// Fixed, but within the minute: not tried yet.
	require.NoError(t, os.Remove(blocker))
	now = evening.Add(59 * time.Second)
	_, _ = w.Write([]byte("still lost\n"))
	assert.NoDirExists(t, blocker)

	now = evening.Add(time.Minute)
	_, _ = w.Write([]byte("back\n"))
	content, err := os.ReadFile(filepath.Join(blocker, "api-20261020.log"))
	require.NoError(t, err)
	assert.Equal(t, "back\n", string(content))
	assert.Equal(t, 1, strings.Count(notes.String(), " WARNING api "), notes.String())
	assert.Contains(t, notes.String(), " INFO api Writing the log file in "+blocker+" again\n")
}

// A write that fails (a full disk) gives the file up for a minute, not until
// the next day.
func TestDailyFileRetriesAfterAFailedWrite(t *testing.T) {
	notes := captureStderr(t)
	dir := t.TempDir()
	now := evening
	w := &dailyFile{dir: dir, name: "api", keep: 14, now: func() time.Time { return now }}
	_, _ = w.Write([]byte("first\n"))

	// The file breaks under it.
	require.NoError(t, w.file.Close())
	_, _ = w.Write([]byte("lost\n"))
	now = evening.Add(30 * time.Second)
	_, _ = w.Write([]byte("lost too\n"))
	assert.Equal(t, 1, strings.Count(notes.String(), " WARNING api "), notes.String())

	now = evening.Add(90 * time.Second)
	_, _ = w.Write([]byte("back\n"))
	content, err := os.ReadFile(filepath.Join(dir, "api-20261020.log"))
	require.NoError(t, err)
	assert.Equal(t, "first\nback\n", string(content))
	assert.Contains(t, notes.String(), " INFO api Writing the log file in "+dir+" again\n")
}

func TestSetupWritesTheDaysFileAndStderr(t *testing.T) {
	captureLogs(t)
	written := captureStderr(t)
	logrus.SetFormatter(&logrus.TextFormatter{})
	dir := filepath.Join(t.TempDir(), "logs")
	Setup(dir)
	assert.IsType(t, Formatter{}, logrus.StandardLogger().Formatter)
	logrus.Info("Telemetry allow-list reloaded after creating app")

	day := time.Now().UTC().Format("20060102")
	content, err := os.ReadFile(filepath.Join(dir, "api-"+day+".log"))
	require.NoError(t, err)
	assert.Regexp(t, alloyFirstLine, string(content))
	assert.True(t, strings.HasSuffix(string(content), " INFO api Telemetry allow-list reloaded after creating app\n"), string(content))
	assert.Equal(t, string(content), written.String(), "stderr gets every line too")
}

// A LOG_DIR that can't be opened costs the file, not the server: the lines go
// to stderr, and the file is tried again later.
func TestSetupFallsBackToStderr(t *testing.T) {
	logs := captureLogs(t)
	written := captureStderr(t)
	logrus.SetFormatter(&logrus.TextFormatter{})
	blocker := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))

	Setup(filepath.Join(blocker, "logs"))
	assert.Regexp(t, alloyFirstLine, logs.String())
	assert.Contains(t, logs.String(), " WARNING api Logging to stderr only, trying the log file again every minute: ")

	logrus.Info("Still logging")
	assert.True(t, strings.HasSuffix(written.String(), " INFO api Still logging\n"), written.String())
	assert.NotContains(t, written.String(), "WARNING", "the outage was already reported")
}

func TestSetupWithoutDirKeepsTheOutput(t *testing.T) {
	captureLogs(t)
	logrus.SetFormatter(&logrus.TextFormatter{})
	logrus.SetOutput(io.Discard)
	Setup("")
	assert.Equal(t, io.Discard, logrus.StandardLogger().Out)
	assert.IsType(t, Formatter{}, logrus.StandardLogger().Formatter)
}
