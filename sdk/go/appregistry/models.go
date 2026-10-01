package appregistry

import (
	"encoding/json"
	"sort"
	"strings"
)

// CheckOptions contains the typed parameters used to check for updates.
//
// Channel, Platform, and Arch are intentionally user-controlled values. The SDK
// does not detect, normalize, remap, or default them.
type CheckOptions struct {
	Owner   string
	AppName string
	Version string

	Channel  string
	Platform string
	Arch     string

	// DeviceID optionally enables server-side telemetry when supported by the API.
	// When empty, the X-Device-ID header is omitted.
	DeviceID string

	// DownloadToken authenticates update checks of a private app whose download mode is strict.
	// It is scoped to one app and channel and is sent as the X-Download-Token header. A strict app
	// answers a request without it exactly as it answers a request for an app that does not exist.
	DownloadToken string
}

// UpdateResponse contains the typed DPAppRegistry update check response.
type UpdateResponse struct {
	UpdateAvailable        bool   `json:"update_available"`
	UpdateURL              string `json:"update_url,omitempty"`
	Changelog              string `json:"changelog,omitempty"`
	Critical               bool   `json:"critical,omitempty"`
	IsIntermediateRequired bool   `json:"is_intermediate_required,omitempty"`
	PossibleRollback       bool   `json:"possible_rollback,omitempty"`

	// Version is the version of the release the response describes: the update
	// offered (the required intermediate when IsIntermediateRequired), or with
	// no update the newest release, which PossibleRollback points back to. It is
	// set whether or not UpdateAvailable is, so check that first. Empty from a
	// server too old to send it.
	Version string `json:"version,omitempty"`

	// Rollout is set only when the server offered a staged (canary) rollout for the
	// version. When Rollout.Eligible is false the SDK has already forced
	// UpdateAvailable to false and cleared UpdateURL/PackageURLs. It is decoded
	// manually from the raw rollout object, so a malformed one is ignored rather than
	// failing the whole response.
	Rollout *RolloutInfo `json:"-"`

	// PackageURLs contains package-specific URLs decoded from fields such as
	// update_url_deb, update_url_rpm, or any future update_url_<package> key.
	PackageURLs []PackageUpdateURL `json:"-"`

	// Source identifies whether the response came from the edge or API fallback.
	Source UpdateSource `json:"-"`
}

// PackageUpdateURL contains one package-specific update URL.
type PackageUpdateURL struct {
	Package string
	URL     string
}

// UnmarshalJSON decodes fixed response fields and dynamic update_url_<package>
// fields into a typed representation.
func (r *UpdateResponse) UnmarshalJSON(data []byte) error {
	type responseAlias UpdateResponse

	var fixed responseAlias
	if err := json.Unmarshal(data, &fixed); err != nil {
		return err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	packageURLs := make([]PackageUpdateURL, 0)
	for key, raw := range fields {
		if !strings.HasPrefix(key, "update_url_") {
			continue
		}

		var updateURL string
		if err := json.Unmarshal(raw, &updateURL); err != nil {
			return err
		}

		packageURLs = append(packageURLs, PackageUpdateURL{
			Package: strings.TrimPrefix(key, "update_url_"),
			URL:     updateURL,
		})
	}

	sort.Slice(packageURLs, func(i, j int) bool {
		return packageURLs[i].Package < packageURLs[j].Package
	})

	*r = UpdateResponse(fixed)
	r.PackageURLs = packageURLs
	r.Rollout = parseRollout(fields["rollout"])

	return nil
}

// parseRollout decodes a rollout object, returning nil unless it carries both a numeric
// percent and a non-empty string seed. This mirrors the JS SDK: a missing or malformed
// rollout is treated as no rollout at all (full update, no gating).
func parseRollout(raw json.RawMessage) *RolloutInfo {
	if len(raw) == 0 {
		return nil
	}

	var probe struct {
		Percent *int    `json:"percent"`
		Seed    *string `json:"seed"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	if probe.Percent == nil || probe.Seed == nil || *probe.Seed == "" {
		return nil
	}

	return &RolloutInfo{Percent: *probe.Percent, Seed: *probe.Seed}
}

// applyRollout evaluates a staged rollout against the given device and gates the
// response. When the install is not in the rollout bucket the SDK forces
// UpdateAvailable to false and clears the download URLs, so a caller that inspects
// UpdateURL/PackageURLs instead of UpdateAvailable cannot bypass the gate.
func (r *UpdateResponse) applyRollout(deviceID string) {
	if r.Rollout == nil {
		return
	}

	info := evaluateRollout(r.Rollout.Percent, r.Rollout.Seed, deviceID)
	r.Rollout = &info

	if r.UpdateAvailable && !info.Eligible {
		r.UpdateAvailable = false
		r.UpdateURL = ""
		r.PackageURLs = []PackageUpdateURL{}
	}
}

// UpdateSource identifies where an update response was loaded from.
type UpdateSource int

const (
	// SourceUnknown indicates that the response source is unknown.
	SourceUnknown UpdateSource = iota

	// SourceEdge indicates that the response came from the configured EdgeURL.
	SourceEdge

	// SourceAPI indicates that the response came from the configured BaseURL API.
	SourceAPI
)
