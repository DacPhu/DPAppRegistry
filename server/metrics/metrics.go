// Package metrics counts what the API answers, for Prometheus: requests by
// route template and status, update checks by app, and license activations.
// The collectors are package-level and registered when the package loads, so
// handlers count the same whether StartServer or a test set them up.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

// The result label of dpappregistry_update_checks_total.
const (
	UpdateAvailable = "update"
	NoUpdate        = "no_update"
	UpdateError     = "error"
)

// Other stands in for every label of an update check whose app, channel,
// platform and arch the registry doesn't serve together, so a client can't
// make up series.
const Other = "other"

var updateResults = []string{UpdateAvailable, NoUpdate, UpdateError}

var licenseResults = []string{"active", "revoked", "expired", "not_found", "invalid", "rate_limited", "error"}

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dpappregistry_http_requests_total",
		Help: "Requests the API answered, by method, route template and status.",
	}, []string{"method", "route", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "dpappregistry_http_request_duration_seconds",
		Help: "How long the API took to answer, by method and route template.",
		// Uploads stream for minutes: nginx gives them 600 s.
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 300, 600},
	}, []string{"method", "route"})

	updateChecks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dpappregistry_update_checks_total",
		Help: "Update checks answered, by app (owner/app), channel, platform, arch and result.",
	}, []string{"app", "channel", "platform", "arch", "result"})

	licenseActivations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dpappregistry_license_activations_total",
		Help: "License activations answered, by result.",
	}, []string{"result"})
)

// Series whose labels are known in advance start at 0: rate() and increase()
// take a series' first sample as their base, so one that first appeared at 1
// would hide the event that created it.
func init() {
	prometheus.MustRegister(httpRequests, httpDuration, updateChecks, licenseActivations)
	TrackUpdateChecks(Other, Other, Other, Other)
	for _, result := range licenseResults {
		licenseActivations.WithLabelValues(result)
	}
}

// Middleware counts every request and how long it took, by the template of
// the route it matched, never its path, which would make a series per app,
// file or order. It must run outside gin.Recovery, to see the 500 a panic is
// answered with.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		method, route := Method(c.Request.Method), Route(c)
		httpRequests.WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).Inc()
		httpDuration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	}
}

// Method is a request's method as a label: a standard one, or "other" for
// whatever else a client sends (gin hands an unknown method to its 404
// handler, which runs the middleware too).
func Method(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	}
	return "other"
}

// Route is the template of the route a request matched, or "unmatched".
func Route(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return "unmatched"
}

// TrackRoutes starts the series of every route at 0, as status 200. Call it
// once all the routes are registered.
func TrackRoutes(routes gin.RoutesInfo) {
	for _, route := range routes {
		method := Method(route.Method)
		httpRequests.WithLabelValues(method, route.Path, "200")
		httpDuration.WithLabelValues(method, route.Path)
	}
}

// UpdateCheck counts one answer to an update check. app is owner/app; the
// caller passes Other for all four unless the registry serves that app on
// that channel, platform and arch (handler/info does).
func UpdateCheck(app, channel, platform, arch, result string) {
	updateChecks.WithLabelValues(app, channel, platform, arch, result).Inc()
}

// TrackUpdateChecks starts the series of one app, channel, platform and arch
// at 0, one per result.
func TrackUpdateChecks(app, channel, platform, arch string) {
	for _, result := range updateResults {
		updateChecks.WithLabelValues(app, channel, platform, arch, result)
	}
}

// LicenseActivation counts one answer to a license activation: active,
// revoked, expired, not_found, invalid, rate_limited or error.
func LicenseActivation(result string) {
	licenseActivations.WithLabelValues(result).Inc()
}
