package utils

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setJWTSecret(t *testing.T, secret string) {
	t.Helper()
	previous := viper.GetString("JWT_SECRET")
	viper.Set("JWT_SECRET", secret)
	t.Cleanup(func() { viper.Set("JWT_SECRET", previous) })
}

func signToken(t *testing.T, claims jwt.MapClaims, key string) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	require.NoError(t, err)
	return token
}

func TestUsernameFromJWTAcceptsIssuedToken(t *testing.T) {
	setJWTSecret(t, "test-secret")
	token, err := GenerateJWT("admin")
	require.NoError(t, err)

	username, err := UsernameFromJWT(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", username)
}

func TestUsernameFromJWTRejectsTokenWhenSecretUnset(t *testing.T) {
	setJWTSecret(t, "")
	forged := signToken(t, jwt.MapClaims{"username": "admin", "exp": time.Now().Add(time.Hour).Unix()}, "")

	_, err := UsernameFromJWT(forged)
	assert.ErrorIs(t, err, errJWTSecretUnset)
}

func TestUsernameFromJWTRejectsTokenWithoutValidExp(t *testing.T) {
	setJWTSecret(t, "test-secret")
	cases := map[string]jwt.MapClaims{
		"missing": {"username": "admin"},
		"zero":    {"username": "admin", "exp": 0},
	}
	for name, claims := range cases {
		_, err := UsernameFromJWT(signToken(t, claims, "test-secret"))
		assert.ErrorIsf(t, err, errMissingExp, "exp %s", name)
	}
}

func TestGenerateJWTRefusesEmptySecret(t *testing.T) {
	setJWTSecret(t, "")

	_, err := GenerateJWT("admin")
	assert.ErrorIs(t, err, errJWTSecretUnset)
}
