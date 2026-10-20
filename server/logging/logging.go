// Package logging writes the server's log lines in the format the log shipper
// (the infra repo's Alloy) parses, to stderr and to a file a day.
package logging

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sirupsen/logrus"
)

// The files' prefix (api-YYYYMMDD.log): the shipper takes a file's service
// label from it.
const service = "api"

// The days of files kept, today's included.
const keepDays = 14

// loggerField names the logger of an entry, in place of api: the access log's
// lines are http's.
const loggerField = "logger"

// Where every line goes, whatever happens to the files.
var stderr io.Writer = os.Stderr

// Setup sends the server's log lines to stderr in Formatter's format and,
// when dir is set, to dir/api-YYYYMMDD.log as well. A file it can't open or
// write costs the file, never the server: the lines still reach stderr, and
// the file is tried again every minute.
func Setup(dir string) {
	logrus.SetFormatter(Formatter{})
	if dir == "" {
		return
	}
	file := newDailyFile(dir, service, keepDays)
	if err := file.start(); err != nil {
		logrus.Warnf("Logging to stderr only, trying the log file again every minute: %v", err)
	}
	logrus.SetOutput(io.MultiWriter(stderr, file))
}

// Formatter writes each entry as one line:
//
//	2026-10-20T12:00:00Z INFO api <message> key=value...
//
// The time is UTC to the second, the level DEBUG, INFO, WARNING, ERROR or
// CRITICAL, then the logger. The shipper takes a line that starts with a
// timestamp for the start of an entry, and a message can carry a client's
// text, control characters included: every later line of a message is
// indented, so it can't pass for an entry of its own, and every other control
// character is escaped (\r, \t, \x1b...), so none can rewrite a line in a
// terminal following the log. Fields follow sorted, quoted when they need it.
type Formatter struct{}

func (Formatter) Format(entry *logrus.Entry) ([]byte, error) {
	var line bytes.Buffer
	line.WriteString(entry.Time.UTC().Format("2006-01-02T15:04:05Z"))
	line.WriteByte(' ')
	line.WriteString(level(entry.Level))
	line.WriteByte(' ')
	logger, _ := entry.Data[loggerField].(string)
	if logger == "" {
		logger = service
	}
	line.WriteString(logger)
	if message := strings.TrimRight(entry.Message, "\r\n"); message != "" {
		line.WriteByte(' ')
		for _, r := range message {
			switch {
			case r == '\n':
				line.WriteString("\n  ")
			case unicode.IsControl(r):
				escaped := strconv.QuoteRune(r)
				line.WriteString(escaped[1 : len(escaped)-1])
			default:
				line.WriteRune(r)
			}
		}
	}
	keys := make([]string, 0, len(entry.Data))
	for key := range entry.Data {
		if key != loggerField {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		line.WriteByte(' ')
		line.WriteString(quote(key))
		line.WriteByte('=')
		line.WriteString(quote(fmt.Sprint(entry.Data[key])))
	}
	line.WriteByte('\n')
	return line.Bytes(), nil
}

func level(l logrus.Level) string {
	switch l {
	case logrus.TraceLevel, logrus.DebugLevel:
		return "DEBUG"
	case logrus.InfoLevel:
		return "INFO"
	case logrus.WarnLevel:
		return "WARNING"
	case logrus.ErrorLevel:
		return "ERROR"
	}
	return "CRITICAL" // fatal and panic
}

// quote leaves a plain value bare. One that is empty, or holds a space, "=",
// a quote, a backslash, a control character or invalid UTF-8, is written as a
// quoted string, so it stays one field and can't forge another.
func quote(value string) string {
	plain := value != "" && !strings.ContainsFunc(value, func(r rune) bool {
		return r == ' ' || r == '=' || r == '"' || r == '\\' || r == utf8.RuneError || !unicode.IsPrint(r)
	})
	if plain {
		return value
	}
	return strconv.Quote(value)
}
