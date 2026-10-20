package logging

import (
	"regexp"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/metrics"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// What an X-Request-ID must look like to be logged as the trace (nginx's
// $request_id is 32 hex digits). A client can send its own: a space or "=" in
// it would forge fields, so any other value is logged as "-".
var requestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// AccessLog writes a line for each request once it is answered:
//
//	<time> INFO http trace=<X-Request-ID|-> method=GET route=/checkVersion path=/checkVersion status=200 ms=12 bytes=123 ip=203.0.113.9
//
// The path is the escaped one, without the query string, which carries
// secrets (an order's view token, a download key). /health isn't logged.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.EscapedPath()
		c.Next()
		route := metrics.Route(c)
		if route == "/health" {
			return
		}
		trace := c.GetHeader("X-Request-ID")
		if !requestID.MatchString(trace) {
			trace = "-"
		}
		ip := c.ClientIP()
		if ip == "" {
			ip = "-"
		}
		logrus.WithField(loggerField, "http").Infof("trace=%s method=%s route=%s path=%s status=%d ms=%d bytes=%d ip=%s",
			trace, metrics.Method(c.Request.Method), route, quote(path), c.Writer.Status(),
			time.Since(start).Milliseconds(), max(c.Writer.Size(), 0), ip)
	}
}
