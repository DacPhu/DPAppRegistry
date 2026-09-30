package appregistry

import "regexp"

// release matches the versions DPAppRegistry accepts: numbers joined by dots or
// dashes. A development build's version, such as "dev" or a commit hash, has
// nothing to compare against.
var release = regexp.MustCompile(`^[0-9]+([.-][0-9]+)*$`)

// IsRelease reports whether version can be checked for updates.
func IsRelease(version string) bool { return release.MatchString(version) }
