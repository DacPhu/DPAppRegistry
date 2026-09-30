package appregistry

import (
	"crypto/sha256"
	"encoding/binary"
)

// RolloutInfo describes a staged (canary) rollout decision for the offered version.
//
// It is present on UpdateResponse only when the server offered a rollout below 100%.
// When Eligible is false the SDK has already forced UpdateAvailable to false and
// cleared the download URLs.
type RolloutInfo struct {
	Percent int    `json:"percent"`
	Seed    string `json:"seed"`

	// Bucket is the deterministic bucket in [0, 99] for this device, or nil when no
	// DeviceID was supplied and the bucket could not be computed.
	Bucket   *int `json:"-"`
	Eligible bool `json:"-"`
}

// RolloutBucket maps a device to a deterministic bucket in [0, 99]:
// sha256(deviceID + ":" + seed), first 8 bytes as a big-endian uint64, modulo 100.
//
// This is the reference algorithm every DPAppRegistry SDK must replicate byte-for-byte so
// a device's rollout decision matches across SDKs.
func RolloutBucket(deviceID, seed string) int {
	sum := sha256.Sum256([]byte(deviceID + ":" + seed))
	return int(binary.BigEndian.Uint64(sum[:8]) % 100)
}

// evaluateRollout decides whether this install is inside a staged rollout. Without a
// DeviceID the bucket cannot be computed, so the install stays out of the canary.
func evaluateRollout(percent int, seed, deviceID string) RolloutInfo {
	if deviceID == "" {
		return RolloutInfo{Percent: percent, Seed: seed, Bucket: nil, Eligible: false}
	}

	bucket := RolloutBucket(deviceID, seed)
	return RolloutInfo{Percent: percent, Seed: seed, Bucket: &bucket, Eligible: bucket < percent}
}
