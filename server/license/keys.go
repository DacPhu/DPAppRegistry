// Package license issues and keeps the license keys a registry sells for its
// products, signed with the registry's Ed25519 key. Apps verify them offline
// with sdk/go/licensekey and the public key.
package license

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/url"

	"github.com/DacPhu/DPAppRegistry/sdk/go/licensekey"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// ErrDisabled is any attempt to issue a license on a registry with no signing key.
var ErrDisabled = errors.New("license issuing is off: LICENSE_SIGNING_KEY is not set")

var signer ed25519.PrivateKey

// Setup loads the signing key from its hex seed (LICENSE_SIGNING_KEY). With an
// empty seed, issuing stays off and every Issue returns ErrDisabled.
func Setup(seedHex string) error {
	if seedHex == "" {
		signer = nil
		logrus.Warn("LICENSE_SIGNING_KEY is not set; licenses cannot be issued")
		return nil
	}
	key, err := licensekey.PrivateKeyFromSeedHex(seedHex)
	if err != nil {
		signer = nil
		return err
	}
	signer = key
	logrus.Infof("License signing key loaded (kid %s)", KeyID())
	return nil
}

// Enabled reports whether this registry can issue and verify licenses.
func Enabled() bool { return signer != nil }

// PublicKey verifies the licenses this registry issues; nil when disabled.
func PublicKey() ed25519.PublicKey {
	if signer == nil {
		return nil
	}
	return signer.Public().(ed25519.PublicKey)
}

// PublicKeyHex is the public key as apps embed it.
func PublicKeyHex() string {
	if signer == nil {
		return ""
	}
	return hex.EncodeToString(PublicKey())
}

// KeyID names the signing key the way licenses do.
func KeyID() string {
	if signer == nil {
		return ""
	}
	return licensekey.KeyID(PublicKey())
}

// Issuer is the name licenses carry for the registry: the store's host.
func Issuer() string {
	if u, err := url.Parse(viper.GetString("STORE_URL")); err == nil && u.Host != "" {
		return u.Host
	}
	return "dpappregistry"
}
