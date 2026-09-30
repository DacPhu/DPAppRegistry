package appregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckForUpdatesSendsDownloadToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(DownloadTokenHeader) != "fnd_secret" {
			t.Fatalf("unexpected download token: %q", r.Header.Get(DownloadTokenHeader))
		}
		writeJSON(t, w, UpdateResponse{UpdateAvailable: true})
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	if _, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner:         "admin",
		AppName:       "test",
		Version:       "0.0.0.5",
		Channel:       "nightly",
		DownloadToken: "fnd_secret",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckForUpdatesOmitsDownloadTokenHeaderWhenUnset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header[http.CanonicalHeaderKey(DownloadTokenHeader)]; ok {
			t.Fatal("download token header must be omitted when no token is set")
		}
		writeJSON(t, w, UpdateResponse{UpdateAvailable: true})
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	if _, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner:   "admin",
		AppName: "test",
		Version: "0.0.0.5",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A private app is never published to the edge, so the token means the edge would only cost a request.
func TestCheckForUpdatesSkipsEdgeWithDownloadToken(t *testing.T) {
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("edge must not be requested: %s", r.URL.Path)
	}))
	defer edge.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, UpdateResponse{UpdateAvailable: true})
	}))
	defer api.Close()

	client := NewClient(Config{BaseURL: api.URL, EdgeURL: edge.URL})
	resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner:         "admin",
		AppName:       "test",
		Version:       "0.0.0.5",
		Channel:       "nightly",
		Platform:      "darwin",
		Arch:          "arm64",
		DownloadToken: "fnd_secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Source != SourceAPI {
		t.Fatalf("unexpected source: %v", resp.Source)
	}
}

func TestStripDownloadTokenOnRedirect(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(DownloadTokenHeader); got != "" {
			t.Fatalf("the token reached storage: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer storage.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(DownloadTokenHeader) != "fnd_secret" {
			t.Fatalf("the token must reach the API: %q", r.Header.Get(DownloadTokenHeader))
		}
		http.Redirect(w, r, storage.URL+"/object", http.StatusFound)
	}))
	defer api.Close()

	req, err := http.NewRequest(http.MethodGet, api.URL+"/download?key=app/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(DownloadTokenHeader, "fnd_secret")

	client := &http.Client{CheckRedirect: StripDownloadTokenOnRedirect}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", res.StatusCode)
	}
}
