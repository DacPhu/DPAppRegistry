package appregistry

import (
	"net/http"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	userAgent      = "dpappregistry-go/1.0"
)

// Client is a concurrency-safe DPAppRegistry SDK client.
type Client struct {
	baseURL    string
	edgeURL    string
	httpClient *http.Client
}

// NewClient creates a new DPAppRegistry SDK client.
//
// The returned client is safe for concurrent use. If cfg.HTTPClient is nil, the
// SDK creates an HTTP client with a default timeout and reusable connections.
func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{
		baseURL:    cfg.BaseURL,
		edgeURL:    cfg.EdgeURL,
		httpClient: httpClient,
	}
}
