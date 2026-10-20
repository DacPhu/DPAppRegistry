package metrics

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsAnswerOnlyAllowedClients(t *testing.T) {
	allow, err := ParseAllow("172.16.0.0/12,127.0.0.0/8")
	require.NoError(t, err)
	metrics := handler(allow)

	for remote, want := range map[string]int{
		"127.0.0.1:50000":           http.StatusOK,
		"172.18.0.5:50000":          http.StatusOK, // Prometheus on the infra network
		"172.30.0.1:50000":          http.StatusOK, // the stack's gateway, after Docker's NAT
		"[::ffff:172.18.0.5]:50000": http.StatusOK,
		"203.0.113.9:50000":         http.StatusForbidden,
		"192.168.1.10:50000":        http.StatusForbidden,
		"[2001:db8::1]:50000":       http.StatusForbidden,
		"[::1]:50000":               http.StatusForbidden, // not in this list
		"not an address":            http.StatusForbidden,
	} {
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.RemoteAddr = remote
		// Only the connection's address counts, never a header's claim.
		request.Header.Set("X-Forwarded-For", "127.0.0.1")
		request.Header.Set("X-Real-IP", "127.0.0.1")
		recorder := httptest.NewRecorder()
		metrics.ServeHTTP(recorder, request)
		assert.Equal(t, want, recorder.Code, remote)
		if want == http.StatusOK {
			assert.Contains(t, recorder.Body.String(), "dpappregistry_http_requests_total", remote)
			assert.Contains(t, recorder.Body.String(), "go_goroutines", remote)
		} else {
			assert.NotContains(t, recorder.Body.String(), "dpappregistry", remote)
		}
	}
}

func TestParseAllow(t *testing.T) {
	loopback, err := ParseAllow(" ")
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}, loopback)

	masked, err := ParseAllow("172.16.0.0/12, 10.1.2.3/8")
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("10.0.0.0/8")}, masked)

	for _, allow := range []string{"172.16.0.0/12,not-a-cidr", "10.0.0.1", "172.16.0.0/12,"} {
		_, err := ParseAllow(allow)
		assert.Error(t, err, allow)
	}
}

func TestServe(t *testing.T) {
	// No address, no listener, and its allow-list isn't even read.
	assert.NoError(t, Serve("", "not-a-cidr"))
	assert.Error(t, Serve("127.0.0.1:0", "not-a-cidr"))

	// A real listener, open to the loopback by default.
	free, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := free.Addr().String()
	require.NoError(t, free.Close())
	require.NoError(t, Serve(addr, ""))
	assert.Error(t, Serve(addr, ""), "a taken address is an error")

	response, err := http.Get("http://" + addr + "/metrics")
	require.NoError(t, err)
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, string(body), "dpappregistry_update_checks_total")

	for path, want := range map[string]int{"/": http.StatusNotFound, "/metrics/x": http.StatusNotFound} {
		response, err := http.Get("http://" + addr + path)
		require.NoError(t, err)
		response.Body.Close()
		assert.Equal(t, want, response.StatusCode, path)
	}
	response, err = http.Post("http://"+addr+"/metrics", "text/plain", nil)
	require.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, response.StatusCode)
}
