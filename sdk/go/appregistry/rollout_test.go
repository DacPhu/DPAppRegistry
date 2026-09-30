package appregistry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const rolloutSeed = "badadc23b08e3943"

func TestRolloutBucketIsDeterministic(t *testing.T) {
	t.Parallel()

	if RolloutBucket("device-1", rolloutSeed) != RolloutBucket("device-1", rolloutSeed) {
		t.Fatal("bucket is not deterministic")
	}
}

func TestRolloutBucketMatchesReference(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"device-1":   18,
		"device-2":   22,
		"device-abc": 5,
	}
	for device, want := range cases {
		if got := RolloutBucket(device, rolloutSeed); got != want {
			t.Fatalf("RolloutBucket(%q) = %d, want %d", device, got, want)
		}
	}
}

func TestRolloutBucketWithinRange(t *testing.T) {
	t.Parallel()

	for i := 0; i < 500; i++ {
		b := RolloutBucket(fmt.Sprintf("device-%d", i), rolloutSeed)
		if b < 0 || b >= 100 {
			t.Fatalf("bucket out of range: %d", b)
		}
	}
}

func TestEvaluateRollout(t *testing.T) {
	t.Parallel()

	in := evaluateRollout(20, rolloutSeed, "device-1") // bucket 18
	if in.Bucket == nil || *in.Bucket != 18 || !in.Eligible {
		t.Fatalf("expected included with bucket 18, got %+v", in)
	}

	out := evaluateRollout(20, rolloutSeed, "device-2") // bucket 22
	if out.Bucket == nil || *out.Bucket != 22 || out.Eligible {
		t.Fatalf("expected excluded with bucket 22, got %+v", out)
	}

	// Sticky when the percent is raised.
	if !evaluateRollout(50, rolloutSeed, "device-1").Eligible {
		t.Fatal("device-1 should stay eligible at 50%")
	}

	none := evaluateRollout(20, rolloutSeed, "")
	if none.Bucket != nil || none.Eligible {
		t.Fatalf("expected excluded with nil bucket, got %+v", none)
	}
}

func rolloutBody(percent int, extraURL bool) string {
	dmg := ""
	if extraURL {
		dmg = `"update_url_dmg":"https://downloads.example/app.dmg",`
	}
	return fmt.Sprintf(
		`{"update_available":true,"update_url":"https://downloads.example/app",%s"rollout":{"percent":%d,"seed":%q}}`,
		dmg, percent, rolloutSeed,
	)
}

func TestCheckForUpdatesKeepsUpdateInsideRolloutBucket(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeRawJSON(t, w, rolloutBody(20, false))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner: "admin", AppName: "test", Version: "0.0.0.5", DeviceID: "device-1",
	})
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}
	if !resp.UpdateAvailable || resp.UpdateURL != "https://downloads.example/app" {
		t.Fatalf("expected update available, got %+v", resp)
	}
	if resp.Rollout == nil || resp.Rollout.Bucket == nil || *resp.Rollout.Bucket != 18 || !resp.Rollout.Eligible {
		t.Fatalf("unexpected rollout: %+v", resp.Rollout)
	}
}

func TestCheckForUpdatesGatesAndBlanksURLsOutsideBucket(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeRawJSON(t, w, rolloutBody(20, true))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner: "admin", AppName: "test", Version: "0.0.0.5", DeviceID: "device-2",
	})
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}
	if resp.UpdateAvailable {
		t.Fatal("expected update to be gated")
	}
	if resp.UpdateURL != "" {
		t.Fatalf("expected blanked UpdateURL, got %q", resp.UpdateURL)
	}
	if len(resp.PackageURLs) != 0 {
		t.Fatalf("expected blanked PackageURLs, got %+v", resp.PackageURLs)
	}
	if resp.Rollout == nil || resp.Rollout.Bucket == nil || *resp.Rollout.Bucket != 22 || resp.Rollout.Eligible {
		t.Fatalf("unexpected rollout: %+v", resp.Rollout)
	}
}

func TestCheckForUpdatesGatesWithoutDeviceID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeRawJSON(t, w, rolloutBody(20, false))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner: "admin", AppName: "test", Version: "0.0.0.5",
	})
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}
	if resp.UpdateAvailable || resp.UpdateURL != "" {
		t.Fatalf("expected gated update, got %+v", resp)
	}
	if resp.Rollout == nil || resp.Rollout.Bucket != nil || resp.Rollout.Eligible {
		t.Fatalf("unexpected rollout: %+v", resp.Rollout)
	}
}

func TestCheckForUpdatesIgnoresMalformedRollout(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"missing seed":      `{"update_available":true,"update_url":"https://downloads.example/app","rollout":{"percent":20}}`,
		"empty seed":        `{"update_available":true,"update_url":"https://downloads.example/app","rollout":{"percent":20,"seed":""}}`,
		"missing percent":   `{"update_available":true,"update_url":"https://downloads.example/app","rollout":{"seed":"badadc23b08e3943"}}`,
		"percent as string": `{"update_available":true,"update_url":"https://downloads.example/app","rollout":{"percent":"20","seed":"badadc23b08e3943"}}`,
	}

	for name, body := range cases {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeRawJSON(t, w, body)
			}))
			defer server.Close()

			client := NewClient(Config{BaseURL: server.URL})
			resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
				Owner: "admin", AppName: "test", Version: "0.0.0.5", DeviceID: "device-2",
			})
			if err != nil {
				t.Fatalf("CheckForUpdates returned error: %v", err)
			}
			if !resp.UpdateAvailable || resp.UpdateURL == "" {
				t.Fatalf("expected untouched full update, got %+v", resp)
			}
			if resp.Rollout != nil {
				t.Fatalf("expected nil rollout for malformed object, got %+v", resp.Rollout)
			}
		})
	}
}

func TestCheckForUpdatesWithoutRolloutIsUntouched(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, UpdateResponse{UpdateAvailable: true, UpdateURL: "https://downloads.example/app"})
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	resp, err := client.CheckForUpdates(context.Background(), CheckOptions{
		Owner: "admin", AppName: "test", Version: "0.0.0.5", DeviceID: "device-2",
	})
	if err != nil {
		t.Fatalf("CheckForUpdates returned error: %v", err)
	}
	if !resp.UpdateAvailable || resp.Rollout != nil {
		t.Fatalf("expected untouched response, got %+v", resp)
	}
}
