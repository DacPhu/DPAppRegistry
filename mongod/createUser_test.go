package mongod

import (
	"strings"
	"testing"

	"github.com/DacPhu/DPAppRegistry/server/model"
)

// bcrypt cannot hash more than 72 bytes. That failure must come back as an error: it used to exit the process.
func TestCreateUserReturnsErrorForOverlongPassword(t *testing.T) {
	creds := &model.Credentials{Username: "admin", Password: strings.Repeat("a", 73)}
	if err := CreateUser(nil, nil, creds); err == nil {
		t.Fatal("expected an error for a password longer than 72 bytes")
	}
}
