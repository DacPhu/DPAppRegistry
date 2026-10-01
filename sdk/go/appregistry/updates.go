package appregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CheckForUpdates checks whether an update is available for the provided app.
//
// If Config.EdgeURL is configured, the client first tries the static edge JSON
// response and falls back to the BaseURL API when the edge misses or fails.
func (c *Client) CheckForUpdates(ctx context.Context, opts CheckOptions) (*UpdateResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := c.validateConfig(); err != nil {
		return nil, err
	}
	if err := validateCheckOptions(opts); err != nil {
		return nil, err
	}

	var edgeErr error
	// A private app cannot be served from the edge (DPAppRegistry refuses cdn_edge for private apps), so a
	// token means the edge lookup is a guaranteed miss and only costs a request.
	if c.edgeURL != "" && opts.DownloadToken == "" {
		resp, err := c.checkEdge(ctx, opts)
		if err == nil {
			if opts.DeviceID != "" {
				_ = c.sendEdgeTelemetryBeacon(ctx, opts, !resp.UpdateAvailable)
			}
			resp.Source = SourceEdge
			return resp, nil
		}
		edgeErr = err
		if ctx.Err() != nil {
			return nil, &CheckError{EdgeErr: edgeErr}
		}
	}

	resp, apiErr := c.checkAPI(ctx, opts)
	if apiErr == nil {
		resp.Source = SourceAPI
		return resp, nil
	}

	return nil, &CheckError{
		EdgeErr: edgeErr,
		APIErr:  apiErr,
	}
}

func (c *Client) validateConfig() error {
	if strings.TrimSpace(c.baseURL) == "" {
		return ErrMissingBaseURL
	}

	if _, err := parseAbsoluteURL(c.baseURL, ErrInvalidBaseURL); err != nil {
		return err
	}

	return nil
}

func validateCheckOptions(opts CheckOptions) error {
	switch {
	case opts.Owner == "":
		return ErrMissingOwner
	case opts.AppName == "":
		return ErrMissingAppName
	case opts.Version == "":
		return ErrMissingVersion
	default:
		return nil
	}
}

func (c *Client) checkEdge(ctx context.Context, opts CheckOptions) (*UpdateResponse, error) {
	endpoint, err := c.edgeCheckURL(opts)
	if err != nil {
		return nil, &EndpointError{Source: SourceEdge, URL: c.edgeURL, Err: err}
	}

	return c.doUpdateRequest(ctx, http.MethodGet, endpoint, opts, SourceEdge)
}

func (c *Client) checkAPI(ctx context.Context, opts CheckOptions) (*UpdateResponse, error) {
	endpoint, err := c.apiCheckURL(opts)
	if err != nil {
		return nil, &EndpointError{Source: SourceAPI, URL: c.baseURL, Err: err}
	}

	return c.doUpdateRequest(ctx, http.MethodGet, endpoint, opts, SourceAPI)
}

func (c *Client) doUpdateRequest(ctx context.Context, method, endpoint string, opts CheckOptions, source UpdateSource) (*UpdateResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, &EndpointError{Source: source, URL: endpoint, Err: err}
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if opts.DeviceID != "" {
		req.Header.Set("X-Device-ID", opts.DeviceID)
	}
	if opts.DownloadToken != "" {
		req.Header.Set(DownloadTokenHeader, opts.DownloadToken)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &EndpointError{Source: source, URL: endpoint, Err: err}
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg := serverMessage(res.Body)
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, &EndpointError{Source: source, URL: endpoint, StatusCode: res.StatusCode, Err: msg}
	}

	var updateResp UpdateResponse
	if err := json.NewDecoder(res.Body).Decode(&updateResp); err != nil {
		return nil, &EndpointError{Source: source, URL: endpoint, Err: fmt.Errorf("decode response: %w", err)}
	}

	updateResp.applyRollout(opts.DeviceID)

	return &updateResp, nil
}

// serverMessage reads the reason DPAppRegistry gives in a failed response's
// body, {"error": "..."}, such as an app it does not know. Nil when there is
// none.
func serverMessage(body io.Reader) error {
	var payload struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(body, 4096))
	if json.Unmarshal(raw, &payload) != nil || strings.TrimSpace(payload.Error) == "" {
		return nil
	}
	return errors.New(strings.TrimSpace(payload.Error))
}

func (c *Client) apiCheckURL(opts CheckOptions) (string, error) {
	u, err := parseAbsoluteURL(c.baseURL, ErrInvalidBaseURL)
	if err != nil {
		return "", err
	}

	u.Path = joinURLPath(u.Path, "checkVersion")
	u.RawPath = ""

	values := u.Query()
	values.Set("app_name", opts.AppName)
	values.Set("version", opts.Version)
	values.Set("channel", opts.Channel)
	values.Set("platform", opts.Platform)
	values.Set("arch", opts.Arch)
	values.Set("owner", opts.Owner)
	u.RawQuery = values.Encode()

	return u.String(), nil
}

func (c *Client) edgeCheckURL(opts CheckOptions) (string, error) {
	u, err := parseAbsoluteURL(c.edgeURL, ErrInvalidEdgeURL)
	if err != nil {
		return "", err
	}

	segments := []string{
		"responses",
		opts.Owner,
		opts.AppName,
		opts.Channel,
		opts.Platform,
		opts.Arch,
		"manual",
		opts.Version + ".json",
	}
	appendEscapedPath(u, segments)
	u.RawQuery = ""

	return u.String(), nil
}

func (c *Client) edgeTelemetryBeaconURL(opts CheckOptions, isLatest bool) (string, error) {
	u, err := parseAbsoluteURL(c.baseURL, ErrInvalidBaseURL)
	if err != nil {
		return "", err
	}

	u.Path = joinURLPath(u.Path, "telemetry/beacon")
	u.RawPath = ""

	values := u.Query()
	values.Set("app_name", opts.AppName)
	values.Set("version", opts.Version)
	values.Set("channel", opts.Channel)
	values.Set("platform", opts.Platform)
	values.Set("arch", opts.Arch)
	values.Set("owner", opts.Owner)
	values.Set("is_latest", strconv.FormatBool(isLatest))
	u.RawQuery = values.Encode()

	return u.String(), nil
}

func (c *Client) sendEdgeTelemetryBeacon(ctx context.Context, opts CheckOptions, isLatest bool) error {
	endpoint, err := c.edgeTelemetryBeaconURL(opts, isLatest)
	if err != nil {
		return &EndpointError{Source: SourceEdge, URL: c.baseURL, Err: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &EndpointError{Source: SourceEdge, URL: endpoint, Err: err}
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Device-ID", opts.DeviceID)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return &EndpointError{Source: SourceEdge, URL: endpoint, Err: err}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)

	if res.StatusCode != http.StatusOK {
		return &EndpointError{Source: SourceEdge, URL: endpoint, StatusCode: res.StatusCode}
	}

	return nil
}

func parseAbsoluteURL(raw string, sentinel error) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", sentinel, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, sentinel
	}
	return u, nil
}

func joinURLPath(basePath, segment string) string {
	if basePath == "" || basePath == "/" {
		return "/" + segment
	}
	return strings.TrimRight(basePath, "/") + "/" + segment
}

func appendEscapedPath(u *url.URL, segments []string) {
	escapedSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		escapedSegments = append(escapedSegments, url.PathEscape(segment))
	}

	escapedBase := strings.TrimRight(u.EscapedPath(), "/")
	decodedBase := strings.TrimRight(u.Path, "/")
	escapedSuffix := strings.Join(escapedSegments, "/")
	decodedSuffix := strings.Join(segments, "/")

	if escapedBase == "" {
		u.Path = "/" + decodedSuffix
		u.RawPath = "/" + escapedSuffix
		return
	}

	u.Path = decodedBase + "/" + decodedSuffix
	u.RawPath = escapedBase + "/" + escapedSuffix
}
