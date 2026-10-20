package info

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DacPhu/DPAppRegistry/server/metrics"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateChecks reads dpappregistry_update_checks_total as Prometheus would
// scrape it, keyed app|channel|platform|arch|result.
func updateChecks(t *testing.T) map[string]float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	found := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "dpappregistry_update_checks_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			key := strings.Join([]string{labels["app"], labels["channel"], labels["platform"], labels["arch"], labels["result"]}, "|")
			found[key] = metric.GetCounter().GetValue()
		}
	}
	return found
}

// useAllowList makes list the telemetry allow-list until the test ends.
func useAllowList(t *testing.T, list *TelemetryAllowList) {
	t.Helper()
	previous := telemetryAllowList.Load()
	telemetryAllowList.Store(list)
	t.Cleanup(func() { telemetryAllowList.Store(previous) })
}

const otherSeries = "other|other|other|other|"

func TestUpdateChecksNameOnlyPublishedSets(t *testing.T) {
	list := NewTelemetryAllowList()
	list.Add("acme", "Desk App", "stable", "darwin", "arm64", "1.2.0")
	useAllowList(t, list)
	trackUpdateChecks(list)

	const desk = "acme/Desk App|stable|darwin|arm64|"
	started := updateChecks(t)
	for _, result := range []string{"update", "no_update", "error"} {
		value, ok := started[desk+result]
		assert.True(t, ok, "the %s series wasn't started", result)
		assert.Zero(t, value)
	}

	known := map[string]interface{}{"owner": "acme", "app_name": "Desk App", "channel": "stable", "platform": "darwin", "arch": "arm64", "version": "1.0.0"}
	countUpdateCheck(known, metrics.UpdateAvailable)
	assert.Equal(t, 1.0, updateChecks(t)[desk+"update"])

	// One value the registry doesn't serve with the others, and all four are "other".
	for _, field := range []string{"owner", "app_name", "channel", "platform", "arch"} {
		params := map[string]interface{}{}
		for key, value := range known {
			params[key] = value
		}
		params[field] = "made-up"
		before := updateChecks(t)[otherSeries+"no_update"]
		countUpdateCheck(params, metrics.NoUpdate)
		assert.Equal(t, before+1, updateChecks(t)[otherSeries+"no_update"], field)
	}

	before := updateChecks(t)
	countUpdateCheck(nil, metrics.UpdateError)
	useAllowList(t, nil) // not loaded yet
	countUpdateCheck(known, metrics.UpdateError)
	after := updateChecks(t)
	assert.Equal(t, before[otherSeries+"error"]+2, after[otherSeries+"error"])
	assert.Equal(t, before[desk+"error"], after[desk+"error"])

	for key := range after {
		assert.NotContains(t, key, "made-up", "a client's value became a label")
		assert.NotContains(t, key, "1.0.0", "a version became a label")
	}
}

func TestTrackUpdateChecksWithoutAllowList(t *testing.T) {
	assert.NotPanics(t, func() { trackUpdateChecks(nil) })
}

// The checks refused before any lookup count as errors, under their app when
// the registry serves it.
func TestFindLatestVersionCountsRefusedChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	list := NewTelemetryAllowList()
	list.Add("acme", "desk", "stable", "darwin", "arm64", "1.2.0")
	useAllowList(t, list)
	trackUpdateChecks(list)

	for target, series := range map[string]string{
		"/checkVersion?owner=acme&app_name=desk&channel=stable&platform=darwin&arch=arm64&version=1.0.0!":    "acme/desk|stable|darwin|arm64|error",
		"/checkVersion?owner=acme&app_name=bad$name&channel=stable&platform=darwin&arch=arm64&version=1.0.0": otherSeries + "error",
	} {
		before := updateChecks(t)[series]
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, target, nil)
		FindLatestVersion(c, nil, nil, nil, false)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, target)
		assert.Equal(t, before+1, updateChecks(t)[series], target)
	}
}
