package appregistry

import (
	"errors"
	"fmt"
)

var (
	// ErrMissingBaseURL is returned when Config.BaseURL is empty.
	ErrMissingBaseURL = errors.New("appregistry: missing base URL")

	// ErrInvalidBaseURL is returned when Config.BaseURL cannot be used as an absolute URL.
	ErrInvalidBaseURL = errors.New("appregistry: invalid base URL")

	// ErrInvalidEdgeURL is returned when Config.EdgeURL cannot be used as an absolute URL.
	ErrInvalidEdgeURL = errors.New("appregistry: invalid edge URL")

	// ErrMissingOwner is returned when CheckOptions.Owner is empty.
	ErrMissingOwner = errors.New("appregistry: missing owner")

	// ErrMissingAppName is returned when CheckOptions.AppName is empty.
	ErrMissingAppName = errors.New("appregistry: missing app name")

	// ErrMissingVersion is returned when CheckOptions.Version is empty.
	ErrMissingVersion = errors.New("appregistry: missing version")

	// ErrRequestFailed is returned when an update check request fails.
	ErrRequestFailed = errors.New("appregistry: request failed")
)

// EndpointError describes a failed request to one DPAppRegistry endpoint.
type EndpointError struct {
	Source     UpdateSource
	URL        string
	StatusCode int
	Err        error
}

// Error returns a human-readable endpoint error message.
func (e *EndpointError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.StatusCode > 0 {
		return fmt.Sprintf("%s: %s returned HTTP %d", ErrRequestFailed, e.URL, e.StatusCode)
	}

	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", ErrRequestFailed, e.URL, e.Err)
	}

	return fmt.Sprintf("%s: %s", ErrRequestFailed, e.URL)
}

// Unwrap returns the underlying endpoint error.
func (e *EndpointError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is reports whether the endpoint error matches a sentinel error.
func (e *EndpointError) Is(target error) bool {
	return target == ErrRequestFailed
}

// CheckError describes a failed update check after all configured endpoints fail.
type CheckError struct {
	EdgeErr error
	APIErr  error
}

// Error returns a human-readable update check error message.
func (e *CheckError) Error() string {
	if e == nil {
		return "<nil>"
	}

	switch {
	case e.EdgeErr != nil && e.APIErr != nil:
		return fmt.Sprintf("%s: edge failed: %v; api failed: %v", ErrRequestFailed, e.EdgeErr, e.APIErr)
	case e.EdgeErr != nil:
		return fmt.Sprintf("%s: edge failed: %v", ErrRequestFailed, e.EdgeErr)
	case e.APIErr != nil:
		return fmt.Sprintf("%s: api failed: %v", ErrRequestFailed, e.APIErr)
	default:
		return ErrRequestFailed.Error()
	}
}

// Unwrap returns all endpoint errors that contributed to the failed check.
func (e *CheckError) Unwrap() []error {
	if e == nil {
		return nil
	}

	errs := make([]error, 0, 2)
	if e.EdgeErr != nil {
		errs = append(errs, e.EdgeErr)
	}
	if e.APIErr != nil {
		errs = append(errs, e.APIErr)
	}
	return errs
}

// Is reports whether the check error matches a sentinel error.
func (e *CheckError) Is(target error) bool {
	return target == ErrRequestFailed
}
