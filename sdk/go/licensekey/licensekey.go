// Package licensekey reads and writes DPAppRegistry license keys: a signed
// statement of what a customer bought, which an app verifies offline with the
// publisher's public key.
//
// A key is one line a person can paste:
//
//	dpl1.<payload>.<signature>
//
// where payload is the base64url (unpadded) JSON of a License and signature
// is the base64url Ed25519 signature of the ASCII bytes "dpl1." + payload. The
// verifier checks the signature over the bytes it received and only then
// decodes the JSON, so no two implementations have to agree on how to
// serialise it. Whitespace anywhere in the key is ignored: mail clients wrap
// long lines.
package licensekey

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Prefix opens every key and names this format.
const Prefix = "dpl1"

// Customer is who the license was issued to.
type Customer struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// License is the signed payload of a key.
type License struct {
	// ID is the license's record in the registry that issued it.
	ID       string   `json:"id"`
	Product  string   `json:"product"`
	Plan     string   `json:"plan"`
	Customer Customer `json:"customer"`
	// Clusters is how many servers (clusters) the license covers; 0 means no limit.
	Clusters int `json:"clusters,omitempty"`
	// ClusterIDs, when set, names the only clusters the license is valid on.
	ClusterIDs []string  `json:"cluster_ids,omitempty"`
	IssuedAt   time.Time `json:"issued_at"`
	// ExpiresAt ends the license term. What an app does after that is up to
	// the app; DPAppRegistry's own products keep working and stop updating.
	ExpiresAt time.Time `json:"expires_at"`
	// UpdatesUntil is the last release date the license entitles the customer
	// to install; zero means every release.
	UpdatesUntil time.Time `json:"updates_until,omitempty"`
	// Issuer is the registry's host, for people reading the key.
	Issuer string `json:"issuer,omitempty"`
	// KeyID names the public key that verifies this license (see KeyID).
	KeyID string `json:"kid,omitempty"`
}

var encoding = base64.RawURLEncoding

// Sign encodes l and signs it with priv, returning the key.
func Sign(l License, priv ed25519.PrivateKey) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("licensekey: private key must be %d bytes", ed25519.PrivateKeySize)
	}
	if l.KeyID == "" {
		l.KeyID = KeyID(priv.Public().(ed25519.PublicKey))
	}
	payload, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	segment := encoding.EncodeToString(payload)
	signature := ed25519.Sign(priv, []byte(Prefix+"."+segment))
	return Prefix + "." + segment + "." + encoding.EncodeToString(signature), nil
}

// Parse verifies key against the given public keys and returns its license.
// It returns ErrFormat for a string that is not a key, ErrSignature when no key
// verifies it, and ErrUnknownKey when keys is empty.
func Parse(key string, keys ...ed25519.PublicKey) (*License, error) {
	if len(keys) == 0 {
		return nil, ErrUnknownKey
	}
	key = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, key)
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] != Prefix || parts[1] == "" || parts[2] == "" {
		return nil, ErrFormat
	}
	signature, err := encoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, ErrFormat
	}
	message := []byte(Prefix + "." + parts[1])
	verified := false
	for _, pub := range keys {
		if len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, message, signature) {
			verified = true
			break
		}
	}
	if !verified {
		return nil, ErrSignature
	}
	payload, err := encoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrFormat
	}
	var l License
	if err := json.Unmarshal(payload, &l); err != nil {
		return nil, ErrFormat
	}
	if l.ID == "" || l.Product == "" || l.Plan == "" || l.IssuedAt.IsZero() || l.ExpiresAt.IsZero() {
		return nil, ErrFormat
	}
	return &l, nil
}

// KeyID names a public key: the first 8 hex digits of its SHA-256, enough to
// tell which of a publisher's keys signed a license.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:4])
}

// PublicKeyFromHex decodes a 32-byte Ed25519 public key written in hex, the
// form publishers hand to their apps.
func PublicKeyFromHex(s string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("licensekey: public key must be %d hex digits", ed25519.PublicKeySize*2)
	}
	return ed25519.PublicKey(raw), nil
}

// PrivateKeyFromSeedHex derives a signing key from its 32-byte seed in hex,
// the form a registry keeps in its configuration.
func PrivateKeyFromSeedHex(s string) (ed25519.PrivateKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != ed25519.SeedSize {
		return nil, fmt.Errorf("licensekey: signing key must be %d hex digits", ed25519.SeedSize*2)
	}
	return ed25519.NewKeyFromSeed(raw), nil
}
