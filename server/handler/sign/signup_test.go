package sign

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func signupWithPassword(t *testing.T, password string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	// No database: an invalid password must be refused before any lookup.
	router.POST("/signup", func(c *gin.Context) { SignUp(c, nil, nil, nil) })
	body := fmt.Sprintf(`{"username":"admin","password":%q,"api_key":"right"}`, password)
	req := httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestSignupRejectsOutOfRangePassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("API_KEY", "right")

	for name, password := range map[string]string{
		"empty":         "",
		"over 72 bytes": strings.Repeat("a", maxPasswordBytes+1),
	} {
		if w := signupWithPassword(t, password); w.Code != http.StatusBadRequest {
			t.Errorf("%s password: expected 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}
}
