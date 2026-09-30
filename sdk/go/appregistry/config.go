package appregistry

import "net/http"

// Config configures a DPAppRegistry SDK client.
type Config struct {
	// BaseURL is the required DPAppRegistry API base URL.
	BaseURL string

	// EdgeURL is an optional static response edge base URL.
	// When configured, the client tries EdgeURL before falling back to BaseURL.
	EdgeURL string

	// HTTPClient is an optional HTTP client.
	// When nil, the SDK creates a client with a reasonable default timeout.
	HTTPClient *http.Client
}
