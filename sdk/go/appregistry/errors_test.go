package appregistry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// checkAgainst runs one update check against a server answering with status
// and body, or against nothing at all when status is 0.
func checkAgainst(t *testing.T, status int, body string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	if status == 0 {
		server.Close() // nothing listens any more: the connection is refused
	} else {
		defer server.Close()
	}
	_, err := NewClient(Config{BaseURL: server.URL}).CheckForUpdates(context.Background(), defaultOptions())
	if err == nil {
		t.Fatal("the check succeeded")
	}
	if !errors.Is(err, ErrRequestFailed) {
		t.Errorf("%v is not ErrRequestFailed", err)
	}
	return err
}

// A refused connection says so once, naming the URL once.
func TestFailedRequestNamesTheURLOnce(t *testing.T) {
	err := checkAgainst(t, 0, "")
	msg := err.Error()
	if !strings.HasPrefix(msg, `appregistry: request failed: Get "`) {
		t.Errorf("message = %q, want it to start with the package prefix and the request", msg)
	}
	if n := strings.Count(msg, "/checkVersion?"); n != 1 {
		t.Errorf("message names the URL %d times: %q", n, msg)
	}
	if strings.Count(msg, "request failed") != 1 {
		t.Errorf("message repeats itself: %q", msg)
	}
}

// The reason the server gives travels with its status.
func TestFailedResponseCarriesTheServersReason(t *testing.T) {
	err := checkAgainst(t, http.StatusBadRequest, `{"error":"no matching documents found for app_name: test"}`)
	if !strings.HasSuffix(err.Error(), " returned HTTP 400: no matching documents found for app_name: test") {
		t.Errorf("message = %q, want the status and the server's reason", err)
	}
	var endpointErr *EndpointError
	if !errors.As(err, &endpointErr) || endpointErr.StatusCode != http.StatusBadRequest {
		t.Errorf("%v does not carry the status as an EndpointError", err)
	}

	// No reason to give: the status alone.
	if err := checkAgainst(t, http.StatusBadGateway, "<html>bad gateway</html>"); !strings.HasSuffix(err.Error(), " returned HTTP 502") {
		t.Errorf("message = %q, want the bare status", err)
	}
}

func TestUndecodableResponseSaysSo(t *testing.T) {
	if err := checkAgainst(t, http.StatusOK, "not json"); !strings.Contains(err.Error(), "decode response: ") {
		t.Errorf("message = %q, want it to say the response could not be decoded", err)
	}
}

// With an edge in front, both endpoints are named.
func TestEdgeAndAPIFailuresAreBothNamed(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	_, err := NewClient(Config{BaseURL: down.URL, EdgeURL: down.URL}).CheckForUpdates(context.Background(), defaultOptions())
	if err == nil || !strings.Contains(err.Error(), "request failed: edge: ") || !strings.Contains(err.Error(), "; api: ") {
		t.Fatalf("error = %v, want both endpoints named", err)
	}
}
