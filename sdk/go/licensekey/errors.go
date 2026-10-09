package licensekey

import "errors"

var (
	// ErrFormat is a string that is not a license key, or a payload missing
	// what every license carries.
	ErrFormat = errors.New("licensekey: not a license key")
	// ErrSignature is a key that none of the given public keys signed.
	ErrSignature = errors.New("licensekey: signature does not verify")
	// ErrUnknownKey is a Parse with no public key to verify against.
	ErrUnknownKey = errors.New("licensekey: no public key to verify with")
	// ErrExpired is a license whose term has ended.
	ErrExpired = errors.New("licensekey: expired")
	// ErrWrongCluster is a license that names clusters, used on another.
	ErrWrongCluster = errors.New("licensekey: not valid on this cluster")
)
