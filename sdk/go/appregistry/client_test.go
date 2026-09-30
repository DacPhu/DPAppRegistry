package appregistry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckForUpdatesUsesBaseAPI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/checkVersion" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("owner") != "admin" {
			t.Fatalf("unexpected owner: %s", r.URL.Query().Get("owner"))
		}
		if r.URL.Query().Get("app_name") != "test" {
			t.Fatalf("unexpected app_name: %s", r.URL.Query().Get("app_name"))
		}
		if r.URL.Query().Get("version") != "0.0.0.5" {
			t.Fatalf("unexpected version: %s", r.URL.Query().Get("version"))
		}
		if r.URL.Query().Get("channel") != "nightly" {
			t.Fatalf("unexpected channel: %s", r.URL.Query().Get("channel"))
		}
		if r.URL.Query().Get("platform") != "darwin" {
			t.Fatalf("unexpected platform: %s", r.URL.Query().Get("platform"))
		}
		if r.URL.Query().Get("arch") != "arm64" {
			t.Fatalf("unexpected arch: %s", r.URL.Query().Get("arch"))
		}
		if r.Header.Get("X-Device-ID") != "device-1" {
			t.Fatalf("unexpected X-Device-ID: %s", r.Header.Get("X-Device-ID"))
		}
		if r.Header.Get("User-Agent") == "" {
			t.Fatal("expected User-Agent header")
		}

		writeJSON(t, w, UpdateResponse{
			UpdateAvailable: true,
			UpdateURL:       "https://downloads.example/app",
		})
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner:    "admin",
		AppName:  "test",
		Version:  "0.0.0.5",
		Channel:  "nightly",
		Platform: "darwin",
		Arch:     "arm64",
		DeviceID: "device-1",
	})
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}

	if !resp.UpdateAvailable {
		t.Fatal("expected update to be available")
	}
	if resp.UpdateURL != "https://downloads.example/app" {
		t.Fatalf("unexpected update URL: %s", resp.UpdateURL)
	}
	if resp.Source != SourceAPI {
		t.Fatalf("unexpected source: %v", resp.Source)
	}
}

func TestCheckForUpdatesUsesEdgeStaticResponse(t *testing.T) {
	t.Parallel()

	apiCalled := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		http.Error(w, "api should not be called", http.StatusInternalServerError)
	}))
	defer apiServer.Close()

	edgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/responses/admin/test/nightly/darwin/arm64/manual/0.0.0.5.json"
		if r.URL.EscapedPath() != wantPath {
			t.Fatalf("unexpected edge path: %s", r.URL.EscapedPath())
		}

		writeJSON(t, w, UpdateResponse{
			UpdateAvailable: false,
		})
	}))
	defer edgeServer.Close()

	client := NewClient(Config{
		BaseURL: apiServer.URL,
		EdgeURL: edgeServer.URL,
	})
	resp, err := client.CheckForUpdates(context.Background(), defaultOptions())
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}

	if resp.Source != SourceEdge {
		t.Fatalf("unexpected source: %v", resp.Source)
	}
	if apiCalled {
		t.Fatal("api fallback should not be called after edge success")
	}
}

func TestCheckForUpdatesSendsTelemetryBeaconAfterEdgeSuccess(t *testing.T) {
	t.Parallel()

	apiCalled := false
	telemetryCalled := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/telemetry/beacon":
			telemetryCalled = true
			if r.URL.Query().Get("app_name") != "test" {
				t.Fatalf("unexpected app_name: %s", r.URL.Query().Get("app_name"))
			}
			if r.URL.Query().Get("version") != "0.0.0.5" {
				t.Fatalf("unexpected version: %s", r.URL.Query().Get("version"))
			}
			if r.URL.Query().Get("channel") != "nightly" {
				t.Fatalf("unexpected channel: %s", r.URL.Query().Get("channel"))
			}
			if r.URL.Query().Get("platform") != "darwin" {
				t.Fatalf("unexpected platform: %s", r.URL.Query().Get("platform"))
			}
			if r.URL.Query().Get("arch") != "arm64" {
				t.Fatalf("unexpected arch: %s", r.URL.Query().Get("arch"))
			}
			if r.URL.Query().Get("owner") != "admin" {
				t.Fatalf("unexpected owner: %s", r.URL.Query().Get("owner"))
			}
			if r.URL.Query().Get("is_latest") != "false" {
				t.Fatalf("unexpected is_latest: %s", r.URL.Query().Get("is_latest"))
			}
			if r.Header.Get("X-Device-ID") != "device-1" {
				t.Fatalf("unexpected X-Device-ID: %s", r.Header.Get("X-Device-ID"))
			}
			w.WriteHeader(http.StatusOK)
		case "/checkVersion":
			apiCalled = true
			http.Error(w, "api should not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()

	edgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, UpdateResponse{
			UpdateAvailable: true,
		})
	}))
	defer edgeServer.Close()

	client := NewClient(Config{
		BaseURL: apiServer.URL,
		EdgeURL: edgeServer.URL,
	})
	resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner:    "admin",
		AppName:  "test",
		Version:  "0.0.0.5",
		Channel:  "nightly",
		Platform: "darwin",
		Arch:     "arm64",
		DeviceID: "device-1",
	})
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}
	if resp.Source != SourceEdge {
		t.Fatalf("unexpected source: %v", resp.Source)
	}
	if !telemetryCalled {
		t.Fatal("expected telemetry beacon call after edge success")
	}
	if apiCalled {
		t.Fatal("api fallback should not be called after edge success")
	}
}

func TestCheckForUpdatesTelemetryBeaconIsLatestFromEdgeResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		updateAvailable bool
		wantIsLatest    string
	}{
		{
			name:            "update available means not latest",
			updateAvailable: true,
			wantIsLatest:    "false",
		},
		{
			name:            "no update means latest",
			updateAvailable: false,
			wantIsLatest:    "true",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			telemetryIsLatest := ""
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/telemetry/beacon" {
					http.NotFound(w, r)
					return
				}
				telemetryIsLatest = r.URL.Query().Get("is_latest")
				w.WriteHeader(http.StatusOK)
			}))
			defer apiServer.Close()

			edgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, UpdateResponse{
					UpdateAvailable: tt.updateAvailable,
				})
			}))
			defer edgeServer.Close()

			client := NewClient(Config{
				BaseURL: apiServer.URL,
				EdgeURL: edgeServer.URL,
			})
			_, err := client.CheckForUpdates(context.Background(), CheckOptions{
				Owner:    "admin",
				AppName:  "test",
				Version:  "0.0.0.5",
				Channel:  "nightly",
				Platform: "darwin",
				Arch:     "arm64",
				DeviceID: "device-1",
			})
			if err != nil {
				t.Fatalf("CheckForUpdates returned error: %v", err)
			}
			if telemetryIsLatest != tt.wantIsLatest {
				t.Fatalf("unexpected is_latest: got %s, want %s", telemetryIsLatest, tt.wantIsLatest)
			}
		})
	}
}

func TestCheckForUpdatesDoesNotSendTelemetryWithoutDeviceID(t *testing.T) {
	t.Parallel()

	telemetryCalled := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/telemetry/beacon" {
			telemetryCalled = true
		}
		http.NotFound(w, r)
	}))
	defer apiServer.Close()

	edgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, UpdateResponse{
			UpdateAvailable: false,
		})
	}))
	defer edgeServer.Close()

	client := NewClient(Config{
		BaseURL: apiServer.URL,
		EdgeURL: edgeServer.URL,
	})
	resp, err := client.CheckForUpdates(context.Background(), defaultOptions())
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}
	if resp.Source != SourceEdge {
		t.Fatalf("unexpected source: %v", resp.Source)
	}
	if telemetryCalled {
		t.Fatal("expected no telemetry beacon call when device id is missing")
	}
}

func TestCheckForUpdatesIgnoresTelemetryFailureAfterEdgeSuccess(t *testing.T) {
	t.Parallel()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/telemetry/beacon" {
			http.Error(w, "telemetry failed", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer apiServer.Close()

	edgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, UpdateResponse{
			UpdateAvailable: true,
			UpdateURL:       "https://downloads.example/app",
		})
	}))
	defer edgeServer.Close()

	client := NewClient(Config{
		BaseURL: apiServer.URL,
		EdgeURL: edgeServer.URL,
	})
	resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner:    "admin",
		AppName:  "test",
		Version:  "0.0.0.5",
		Channel:  "nightly",
		Platform: "darwin",
		Arch:     "arm64",
		DeviceID: "device-1",
	})
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}
	if resp.Source != SourceEdge {
		t.Fatalf("unexpected source: %v", resp.Source)
	}
	if resp.UpdateURL != "https://downloads.example/app" {
		t.Fatalf("unexpected update URL: %s", resp.UpdateURL)
	}
}

func TestCheckForUpdatesFallsBackFromEdge404ToAPI(t *testing.T) {
	t.Parallel()

	edgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer edgeServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, UpdateResponse{
			UpdateAvailable: true,
			UpdateURL:       "https://downloads.example/app",
		})
	}))
	defer apiServer.Close()

	client := NewClient(Config{
		BaseURL: apiServer.URL,
		EdgeURL: edgeServer.URL,
	})
	resp, err := client.CheckForUpdates(context.Background(), defaultOptions())
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}

	if resp.Source != SourceAPI {
		t.Fatalf("unexpected source: %v", resp.Source)
	}
}

func TestCheckForUpdatesFallsBackFromInvalidEdgeJSONToAPI(t *testing.T) {
	t.Parallel()

	edgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer edgeServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, UpdateResponse{UpdateURL: "https://downloads.example/app"})
	}))
	defer apiServer.Close()

	client := NewClient(Config{
		BaseURL: apiServer.URL,
		EdgeURL: edgeServer.URL,
	})
	resp, err := client.CheckForUpdates(context.Background(), defaultOptions())
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}

	if resp.Source != SourceAPI {
		t.Fatalf("unexpected source: %v", resp.Source)
	}
}

func TestCheckForUpdatesDoesNotNormalizeValues(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("channel") != "stable" {
			t.Fatalf("channel was changed: %s", r.URL.Query().Get("channel"))
		}
		if r.URL.Query().Get("platform") != "macos" {
			t.Fatalf("platform was changed: %s", r.URL.Query().Get("platform"))
		}
		if r.URL.Query().Get("arch") != "apple-silicon" {
			t.Fatalf("arch was changed: %s", r.URL.Query().Get("arch"))
		}
		writeJSON(t, w, UpdateResponse{})
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	_, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner:    "admin",
		AppName:  "test",
		Version:  "0.0.0.5",
		Channel:  "stable",
		Platform: "macos",
		Arch:     "apple-silicon",
	})
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}
}

func TestCheckForUpdatesDecodesDynamicPackageURLs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRawJSON(t, w, `{
			"update_available": true,
			"update_url_deb": "https://downloads.example/app.deb",
			"update_url_rpm": "https://downloads.example/app.rpm",
			"changelog": "### Changelog\n\n- Added feature X",
			"critical": true,
			"is_intermediate_required": true,
			"possible_rollback": true
		}`)
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	resp, err := client.CheckForUpdates(context.Background(), defaultOptions())
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}

	if !resp.UpdateAvailable {
		t.Fatal("expected update to be available")
	}
	if resp.Changelog == "" {
		t.Fatal("expected changelog")
	}
	if !resp.Critical {
		t.Fatal("expected critical update")
	}
	if !resp.IsIntermediateRequired {
		t.Fatal("expected intermediate update requirement")
	}
	if !resp.PossibleRollback {
		t.Fatal("expected possible rollback")
	}
	if resp.UpdateURL != "" {
		t.Fatalf("unexpected binary update URL: %s", resp.UpdateURL)
	}
	if len(resp.PackageURLs) != 2 {
		t.Fatalf("expected 2 package URLs, got %d", len(resp.PackageURLs))
	}
	if resp.PackageURLs[0] != (PackageUpdateURL{Package: "deb", URL: "https://downloads.example/app.deb"}) {
		t.Fatalf("unexpected first package URL: %#v", resp.PackageURLs[0])
	}
	if resp.PackageURLs[1] != (PackageUpdateURL{Package: "rpm", URL: "https://downloads.example/app.rpm"}) {
		t.Fatalf("unexpected second package URL: %#v", resp.PackageURLs[1])
	}
}

func TestCheckForUpdatesDecodesBinaryUpdateURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRawJSON(t, w, `{
			"update_available": true,
			"update_url": "https://downloads.example/app"
		}`)
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	resp, err := client.CheckForUpdates(context.Background(), defaultOptions())
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}

	if resp.UpdateURL != "https://downloads.example/app" {
		t.Fatalf("unexpected update URL: %s", resp.UpdateURL)
	}
	if len(resp.PackageURLs) != 0 {
		t.Fatalf("expected no package URLs, got %d", len(resp.PackageURLs))
	}
}

func TestCheckForUpdatesValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		opts CheckOptions
		want error
	}{
		{
			name: "missing base url",
			cfg:  Config{},
			opts: defaultOptions(),
			want: ErrMissingBaseURL,
		},
		{
			name: "missing owner",
			cfg:  Config{BaseURL: "https://api.example"},
			opts: CheckOptions{AppName: "test", Version: "0.0.0.5"},
			want: ErrMissingOwner,
		},
		{
			name: "missing app name",
			cfg:  Config{BaseURL: "https://api.example"},
			opts: CheckOptions{Owner: "admin", Version: "0.0.0.5"},
			want: ErrMissingAppName,
		},
		{
			name: "missing version",
			cfg:  Config{BaseURL: "https://api.example"},
			opts: CheckOptions{Owner: "admin", AppName: "test"},
			want: ErrMissingVersion,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient(tt.cfg)
			_, err := client.CheckForUpdates(context.Background(), tt.opts)
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func defaultOptions() CheckOptions {
	return CheckOptions{
		Owner:    "admin",
		AppName:  "test",
		Version:  "0.0.0.5",
		Channel:  "nightly",
		Platform: "darwin",
		Arch:     "arm64",
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("failed to write json: %v", err)
	}
}

func writeRawJSON(t *testing.T, w http.ResponseWriter, value string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(value))
}
