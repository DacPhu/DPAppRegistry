package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// series reads every series of the named metric from the default registry, as
// Prometheus would scrape it: labels (sorted by name) to value, a histogram's
// value being its count.
func series(t *testing.T, name string) map[string]float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	found := map[string]float64{}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			var labels []string
			for _, label := range metric.GetLabel() {
				labels = append(labels, label.GetName()+"="+label.GetValue())
			}
			value := metric.GetCounter().GetValue()
			if histogram := metric.GetHistogram(); histogram != nil {
				value = float64(histogram.GetSampleCount())
			}
			found[strings.Join(labels, ",")] = value
		}
	}
	return found
}

func TestMiddlewareCountsByRouteTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Middleware(), gin.RecoveryWithWriter(io.Discard))
	router.GET("/apps/:id", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/boom", func(c *gin.Context) { panic("boom") })

	for _, tc := range []struct {
		method, path, label, route string
		status                     int
	}{
		{http.MethodGet, "/apps/42", "GET", "/apps/:id", http.StatusOK},
		{http.MethodGet, "/apps/43?key=secret", "GET", "/apps/:id", http.StatusOK},
		{http.MethodGet, "/nowhere/42", "GET", "unmatched", http.StatusNotFound},
		// gin hands a method it has no route for to its 404 handler, through the middleware.
		{"PROPFIND", "/apps/42", "other", "unmatched", http.StatusNotFound},
		{http.MethodPost, "/apps/42", "POST", "unmatched", http.StatusNotFound},
		// Recovery runs inside the middleware, which sees its 500.
		{http.MethodGet, "/boom", "GET", "/boom", http.StatusInternalServerError},
	} {
		requests := httpRequests.WithLabelValues(tc.label, tc.route, strconv.Itoa(tc.status))
		before := testutil.ToFloat64(requests)
		durationsBefore := series(t, "dpappregistry_http_request_duration_seconds")["method="+tc.label+",route="+tc.route]

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))

		assert.Equal(t, tc.status, recorder.Code, "%s %s", tc.method, tc.path)
		assert.Equal(t, before+1, testutil.ToFloat64(requests), "%s %s", tc.method, tc.path)
		assert.Equal(t, durationsBefore+1, series(t, "dpappregistry_http_request_duration_seconds")["method="+tc.label+",route="+tc.route], "%s %s", tc.method, tc.path)
	}

	for labels := range series(t, "dpappregistry_http_requests_total") {
		assert.NotContains(t, labels, "/apps/4", "a series names a path: %s", labels)
		assert.NotContains(t, labels, "secret", "a series names a query: %s", labels)
	}
}

func TestMethod(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		assert.Equal(t, method, Method(method))
	}
	for _, method := range []string{"PROPFIND", "TRACE", "CONNECT", "get", "", "X-MADE-UP"} {
		assert.Equal(t, "other", Method(method), method)
	}
}

func TestTrackRoutesStartsEveryRouteAtZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/tracked/:owner/*feed", func(c *gin.Context) {})
	router.DELETE("/tracked/delete", func(c *gin.Context) {})
	TrackRoutes(router.Routes())

	requests := series(t, "dpappregistry_http_requests_total")
	durations := series(t, "dpappregistry_http_request_duration_seconds")
	for _, route := range []struct{ method, path string }{{"GET", "/tracked/:owner/*feed"}, {"DELETE", "/tracked/delete"}} {
		value, ok := requests["method="+route.method+",route="+route.path+",status=200"]
		assert.True(t, ok, "no requests series for %s %s", route.method, route.path)
		assert.Zero(t, value)
		value, ok = durations["method="+route.method+",route="+route.path]
		assert.True(t, ok, "no duration series for %s %s", route.method, route.path)
		assert.Zero(t, value)
	}
}

func TestUpdateCheckAndLicenseSeriesStartAtZero(t *testing.T) {
	TrackUpdateChecks("acme/Desk App", "stable", "darwin", "arm64")

	checks := series(t, "dpappregistry_update_checks_total")
	for _, result := range []string{"update", "no_update", "error"} {
		value, ok := checks["app=acme/Desk App,arch=arm64,channel=stable,platform=darwin,result="+result]
		assert.True(t, ok, "no %s series for the tracked app", result)
		assert.Zero(t, value)
		_, ok = checks["app=other,arch=other,channel=other,platform=other,result="+result]
		assert.True(t, ok, "no %s series for other", result)
	}

	activations := series(t, "dpappregistry_license_activations_total")
	for _, result := range []string{"active", "revoked", "expired", "not_found", "invalid", "rate_limited", "error"} {
		_, ok := activations["result="+result]
		assert.True(t, ok, "no license series for %s", result)
	}

	UpdateCheck("acme/Desk App", "stable", "darwin", "arm64", NoUpdate)
	LicenseActivation("expired")
	assert.Equal(t, 1.0, testutil.ToFloat64(updateChecks.WithLabelValues("acme/Desk App", "stable", "darwin", "arm64", "no_update")))
	assert.Equal(t, activations["result=expired"]+1, testutil.ToFloat64(licenseActivations.WithLabelValues("expired")))
}
