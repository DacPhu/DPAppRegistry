package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	appregistry "github.com/DacPhu/DPAppRegistry/sdk/go/appregistry"
)

func main() {
	// Only a private app in strict mode needs a token; an unlisted one is served without it. The token belongs
	// to one app and one channel, and it is an identifier baked into a build rather than a secret: for an
	// internal tool keep it out of the bundle and read it from the environment, as here.
	token := os.Getenv("DPAPPREGISTRY_DOWNLOAD_TOKEN")
	if token == "" {
		fmt.Println("DPAPPREGISTRY_DOWNLOAD_TOKEN is not set: this works for a public or unlisted app, a strict one will answer as if the app did not exist")
	}

	client := appregistry.NewClient(appregistry.Config{
		BaseURL: "http://localhost:9000",
	})

	resp, err := client.CheckForUpdates(context.Background(), appregistry.CheckOptions{
		Owner:         "admin",
		AppName:       "internal-tool",
		Version:       "0.0.0.1",
		Channel:       "stable",
		Platform:      appregistry.SystemPlatform(),
		Arch:          appregistry.SystemArch(),
		DownloadToken: token,
	})
	if err != nil {
		if errors.Is(err, appregistry.ErrRequestFailed) {
			if token == "" {
				log.Printf("a strict private app answers an update check without a token exactly as it answers one for an app that does not exist")
			}
			log.Fatalf("update check request failed: %v", err)
		}
		log.Fatalf("invalid update check options: %v", err)
	}

	printUpdateResponse(resp)

	if !resp.UpdateAvailable || resp.UpdateURL == "" {
		return
	}

	path, err := downloadArtifact(resp.UpdateURL, token)
	if err != nil {
		log.Fatalf("download failed: %v", err)
	}
	fmt.Printf("\nDownloaded to %s\n", path)
}

// downloadArtifact fetches the artifact the update check pointed at. The SDK never downloads anything, so the
// header is set here as well, and StripDownloadTokenOnRedirect keeps it from following the redirect into storage.
func downloadArtifact(updateURL, token string) (string, error) {
	client := &http.Client{
		Timeout:       10 * time.Minute,
		CheckRedirect: appregistry.StripDownloadTokenOnRedirect,
	}

	req, err := http.NewRequest(http.MethodGet, updateURL, nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set(appregistry.DownloadTokenHeader, token)
	}

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		// A missing token on a strict app, or one that does not cover this app and channel, is answered
		// with the same 404 as a key that belongs to no artifact.
		return "", fmt.Errorf("unexpected status %d", res.StatusCode)
	}

	file, err := os.CreateTemp("", "dpappregistry-*"+filepath.Ext(updateURL))
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, res.Body); err != nil {
		return "", err
	}

	return file.Name(), nil
}

func printUpdateResponse(resp *appregistry.UpdateResponse) {
	const pad = "  "

	fmt.Println("-- Update check response --")
	fmt.Printf("%supdate_available:         %t\n", pad, resp.UpdateAvailable)
	fmt.Printf("%scritical:                 %t\n", pad, resp.Critical)
	fmt.Printf("%sis_intermediate_required: %t\n", pad, resp.IsIntermediateRequired)
	fmt.Printf("%spossible_rollback:        %t\n", pad, resp.PossibleRollback)
	fmt.Printf("%ssource:                   %s\n", pad, formatUpdateSource(resp.Source))

	if resp.UpdateURL != "" {
		fmt.Printf("%supdate_url:               %s\n", pad, resp.UpdateURL)
	} else {
		fmt.Printf("%supdate_url:               (empty)\n", pad)
	}

	if resp.Changelog != "" {
		fmt.Printf("%schangelog:\n", pad)
		for _, line := range strings.Split(strings.TrimRight(resp.Changelog, "\n"), "\n") {
			fmt.Printf("%s  %s\n", pad, line)
		}
	} else {
		fmt.Printf("%schangelog:               (empty)\n", pad)
	}

	if len(resp.PackageURLs) == 0 {
		fmt.Printf("%spackage_urls:           (none)\n", pad)
		return
	}

	fmt.Printf("%spackage_urls:\n", pad)
	for _, pkg := range resp.PackageURLs {
		fmt.Printf("%s  %-12s %s\n", pad, pkg.Package+":", pkg.URL)
	}
}

func formatUpdateSource(source appregistry.UpdateSource) string {
	switch source {
	case appregistry.SourceEdge:
		return "edge"
	case appregistry.SourceAPI:
		return "api"
	default:
		return "unknown"
	}
}
