package license

import (
	"net/http"
	"testing"

	"github.com/DacPhu/DPAppRegistry/server/license"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// activationCounts reads dpappregistry_license_activations_total as
// Prometheus would scrape it, by result.
func activationCounts(t *testing.T) map[string]float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	found := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "dpappregistry_license_activations_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "result" {
					found[label.GetValue()] = metric.GetCounter().GetValue()
				}
			}
		}
	}
	return found
}

func TestActivationResult(t *testing.T) {
	for _, tc := range []struct {
		status          int
		license, result string
	}{
		{http.StatusOK, "active", "active"},
		{http.StatusOK, "revoked", "revoked"},
		{http.StatusOK, "expired", "expired"},
		{http.StatusOK, "", "error"}, // a panic before the answer: Recovery's 500 comes later
		{http.StatusBadRequest, "", "invalid"},
		{http.StatusNotFound, "", "not_found"},
		{http.StatusTooManyRequests, "", "rate_limited"},
		{http.StatusInternalServerError, "", "error"},
		{http.StatusServiceUnavailable, "", "error"},
	} {
		assert.Equal(t, tc.result, activationResult(tc.status, tc.license), "%d %q", tc.status, tc.license)
	}
}

// Every answer an activation gets before the database is involved is counted.
func TestActivateCountsItsAnswers(t *testing.T) {
	require.NoError(t, license.Setup(testSeed))
	newRedis := func() *redis.Client { return redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()}) }
	activate := func(rdb *redis.Client) func(*gin.Context) {
		return func(c *gin.Context) { Activate(c, nil, rdb) }
	}
	expect := func(result string, rdb *redis.Client, body string, status int) {
		t.Helper()
		before := activationCounts(t)
		code, _ := call(t, activate(rdb), http.MethodPost, "/store/licenses/activate", body, false)
		assert.Equal(t, status, code, result)
		after := activationCounts(t)
		assert.Equal(t, before[result]+1, after[result], result)
		for other := range before {
			if other != result {
				assert.Equal(t, before[other], after[other], "%s counted as %s too", result, other)
			}
		}
	}

	rdb := newRedis()
	expect("invalid", rdb, `{}`, http.StatusBadRequest)
	expect("invalid", rdb, `{"key":"dpl1.x.y","cluster_id":"not a cluster id"}`, http.StatusBadRequest)
	expect("invalid", rdb, `{"key":"dpl1.x.y","cluster_id":"cluster-a"}`, http.StatusBadRequest)

	limited := newRedis()
	for range activatePerIPPerHour {
		call(t, activate(limited), http.MethodPost, "/store/licenses/activate", `{}`, false)
	}
	expect("rate_limited", limited, `{}`, http.StatusTooManyRequests)

	// Without Redis the rate limiter refuses everything (503).
	stopped := miniredis.RunT(t)
	down := redis.NewClient(&redis.Options{Addr: stopped.Addr(), MaxRetries: -1})
	stopped.Close()
	expect("error", down, `{}`, http.StatusServiceUnavailable)

	// A registry that issues no licenses knows none.
	require.NoError(t, license.Setup(""))
	expect("not_found", newRedis(), `{"key":"dpl1.x.y","cluster_id":"cluster-a"}`, http.StatusNotFound)
}
