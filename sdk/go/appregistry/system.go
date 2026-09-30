package appregistry

import "runtime"

// SystemPlatform returns runtime.GOOS.
//
// The SDK never calls this automatically. It is provided only for callers that
// choose to use Go runtime platform names as their DPAppRegistry platform values.
func SystemPlatform() string {
	return runtime.GOOS
}

// SystemArch returns runtime.GOARCH.
//
// The SDK never calls this automatically. It is provided only for callers that
// choose to use Go runtime architecture names as their DPAppRegistry arch values.
func SystemArch() string {
	return runtime.GOARCH
}
