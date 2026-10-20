package logging

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func accessRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AccessLog(), gin.RecoveryWithWriter(io.Discard))
	router.GET("/apps/:id", func(c *gin.Context) { c.String(http.StatusOK, "hello") })
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/boom", func(c *gin.Context) { panic("boom") })
	return router
}

func serve(router *gin.Engine, target string, header http.Header) {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.RemoteAddr = "203.0.113.9:4000"
	for name, values := range header {
		request.Header[name] = values
	}
	router.ServeHTTP(httptest.NewRecorder(), request)
}

func TestAccessLogLine(t *testing.T) {
	logs := captureLogs(t)
	serve(accessRouter(), "/apps/42?key=secret-download-key&t=order-view-token", http.Header{"X-Request-Id": {"0123456789abcdef0123456789abcdef"}})

	line := logs.String()
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z INFO http trace=0123456789abcdef0123456789abcdef method=GET route=/apps/:id path=/apps/42 status=200 ms=\d+ bytes=5 ip=203\.0\.113\.9\n$`, line)
	assert.Regexp(t, alloyFirstLine, line)
	assert.NotContains(t, line, "secret")
	assert.NotContains(t, line, "token")
	assert.NotContains(t, line, "?")
}

func TestAccessLogOtherAnswers(t *testing.T) {
	router := accessRouter()
	for _, tc := range []struct {
		target, requestID, want string
	}{
		{"/nowhere?x=1", "", "trace=- method=GET route=unmatched path=/nowhere status=404 "},
		{"/boom", "", "trace=- method=GET route=/boom path=/boom status=500 "},
		// A client's own id is logged only if it can't forge a field.
		{"/apps/1", "bad id=forged", "trace=- method=GET route=/apps/:id path=/apps/1 status=200 "},
		{"/apps/1", strings.Repeat("a", 65), "trace=- "},
		{"/apps/1", "ci-run.42_7", "trace=ci-run.42_7 "},
		// "=" may stay unescaped in a path; the path is then quoted, as one field.
		{"/apps/a=b", "", `trace=- method=GET route=/apps/:id path="/apps/a=b" status=200 `},
	} {
		logs := captureLogs(t)
		header := http.Header{}
		if tc.requestID != "" {
			header.Set("X-Request-ID", tc.requestID)
		}
		serve(router, tc.target, header)
		assert.Contains(t, logs.String(), " INFO http "+tc.want, tc.target)
	}
}

func TestAccessLogSkipsHealth(t *testing.T) {
	logs := captureLogs(t)
	serve(accessRouter(), "/health", nil)
	serve(accessRouter(), "/health?probe=1", nil)
	assert.Empty(t, logs.String())
}

// A path whose decoded form holds a newline and a forged entry stays one line:
// the escaped path is logged, so the newline is %0A.
func TestAccessLogPathCantForgeALine(t *testing.T) {
	logs := captureLogs(t)
	serve(accessRouter(), "/apps/x%0A2026-10-20T12:00:00Z%20CRITICAL%20api%20forged%0D", nil)

	line := logs.String()
	require.Equal(t, 1, strings.Count(line, "\n"), line)
	assert.Contains(t, line, " route=/apps/:id path=/apps/x%0A2026-10-20T12:00:00Z%20CRITICAL%20api%20forged%0D status=200 ")
	assert.NotContains(t, line, "\r")
}
