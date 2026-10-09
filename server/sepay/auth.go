package sepay

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxSkew is how old a signed webhook may be. SePay retries a failed delivery
// for about five hours, and it is not documented whether a retry is re-signed.
const MaxSkew = 6 * time.Hour

var (
	ErrNotConfigured = errors.New("sepay: no webhook secret or api key is configured")
	ErrUnauthorized  = errors.New("sepay: webhook authentication failed")
)

// Verify checks that SePay sent the request. With a secret, the HMAC-SHA256
// signature over "<timestamp>.<raw body>" (headers X-SePay-Signature and
// X-SePay-Timestamp) must match and be recent; else the Authorization header
// must carry the API key. body must be the bytes as received.
func Verify(r *http.Request, body []byte, cfg Config, now time.Time) error {
	if cfg.WebhookSecret != "" {
		return verifyHMAC(r, body, cfg.WebhookSecret, now)
	}
	if cfg.WebhookAPIKey != "" {
		return verifyAPIKey(r, cfg.WebhookAPIKey)
	}
	return ErrNotConfigured
}

func verifyHMAC(r *http.Request, body []byte, secret string, now time.Time) error {
	signature := strings.TrimPrefix(r.Header.Get("X-SePay-Signature"), "sha256=")
	timestamp := r.Header.Get("X-SePay-Timestamp")
	if signature == "" || timestamp == "" {
		return ErrUnauthorized
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrUnauthorized
	}
	age := now.Sub(time.Unix(seconds, 0))
	if age > MaxSkew || age < -MaxSkew {
		return ErrUnauthorized
	}
	given, err := hex.DecodeString(signature)
	if err != nil {
		return ErrUnauthorized
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	if !hmac.Equal(given, mac.Sum(nil)) {
		return ErrUnauthorized
	}
	return nil
}

func verifyAPIKey(r *http.Request, key string) error {
	given, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Apikey ")
	if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(given)), []byte(key)) != 1 {
		return ErrUnauthorized
	}
	return nil
}
