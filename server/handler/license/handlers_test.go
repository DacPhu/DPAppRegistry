package license

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DacPhu/DPAppRegistry/sdk/go/licensekey"
	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

const testSeed = "0202020202020202020202020202020202020202020202020202020202020202"

func mongoTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	mongoURL := os.Getenv("MONGODB_URL_TESTS")
	if mongoURL == "" {
		t.Skip("MONGODB_URL_TESTS not set, skipping Mongo-dependent test")
	}
	parsed, err := connstring.ParseAndValidate(mongoURL)
	require.NoError(t, err)
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURL))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client.Database(parsed.Database)
}

// call runs a handler as the admin "lic-admin" (or anonymously) and decodes the JSON answer.
func call(t *testing.T, handle func(*gin.Context), method, target, body string, asAdmin bool) (int, map[string]json.RawMessage) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.RemoteAddr = "203.0.113.9:4000"
	if asAdmin {
		c.Set("username", "lic-admin")
	}
	handle(c)
	var decoded map[string]json.RawMessage
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder.Code, decoded
}

func TestLicenseLifecycleThroughTheAPI(t *testing.T) {
	db := mongoTestDB(t)
	ctx := context.Background()
	cleanup := func() {
		_, _ = db.Collection("licenses").DeleteMany(ctx, bson.M{"owner": "lic-admin"})
		_, _ = db.Collection("license_activations").DeleteMany(ctx, bson.M{"owner": "lic-admin"})
	}
	cleanup()
	t.Cleanup(cleanup)
	require.NoError(t, license.Setup(testSeed))
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})

	create := func(c *gin.Context) { Create(c, db) }
	list := func(c *gin.Context) { List(c, db) }
	show := func(c *gin.Context) { Show(c, db) }
	revoke := func(c *gin.Context) { Revoke(c, db) }
	activate := func(c *gin.Context) { Activate(c, db, rdb) }

	code, body := call(t, create, http.MethodPost, "/license/create", `{"product":"dpfs","plan":"business","customer":{"name":"ABC","email":"it@abc.vn"},"clusters":1,"years":1}`, true)
	require.Equal(t, http.StatusCreated, code, string(body["error"]))
	var key string
	require.NoError(t, json.Unmarshal(body["key"], &key))
	var created model.License
	require.NoError(t, json.Unmarshal(body["license"], &created))
	assert.Equal(t, "lic-admin", created.Owner)

	code, _ = call(t, create, http.MethodPost, "/license/create", `{"product":"dpfs","plan":"business","customer":{"name":"ABC"}}`, true)
	assert.Equal(t, http.StatusBadRequest, code, "no term")
	code, _ = call(t, create, http.MethodPost, "/license/create", `{"product":"dpfs","plan":"community","customer":{"name":"ABC"},"years":1}`, true)
	assert.Equal(t, http.StatusBadRequest, code, "free plan")

	code, body = call(t, list, http.MethodGet, "/license/list?product=dpfs", "", true)
	require.Equal(t, http.StatusOK, code)
	var listed []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body["licenses"], &listed))
	require.Len(t, listed, 1)
	_, hasKey := listed[0]["key"]
	assert.False(t, hasKey, "the list never carries keys")

	code, body = call(t, show, http.MethodGet, "/license/show?id="+created.ID.Hex(), "", true)
	require.Equal(t, http.StatusOK, code)
	_, hasKey = body["key"]
	assert.False(t, hasKey)
	code, body = call(t, show, http.MethodGet, "/license/show?id="+created.ID.Hex()+"&key=1", "", true)
	require.Equal(t, http.StatusOK, code)
	var shownKey string
	require.NoError(t, json.Unmarshal(body["key"], &shownKey))
	assert.Equal(t, key, shownKey)

	// Activation from two clusters, the second over the limit; a tampered key is refused.
	activateBody := func(cluster string) string {
		return `{"key":"` + key + `","cluster_id":"` + cluster + `","version":"1.2.0","platform":"linux","arch":"amd64"}`
	}
	counted := activationCounts(t)
	code, body = call(t, activate, http.MethodPost, "/store/licenses/activate", activateBody("cluster-a"), false)
	require.Equal(t, http.StatusOK, code, string(body["error"]))
	assert.Equal(t, `"active"`, string(body["status"]))
	assert.Equal(t, "1", string(body["clusters_active"]))
	assert.Equal(t, "false", string(body["over"]))
	call(t, activate, http.MethodPost, "/store/licenses/activate", activateBody("cluster-a"), false)
	code, body = call(t, activate, http.MethodPost, "/store/licenses/activate", activateBody("cluster-b"), false)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "2", string(body["clusters_active"]))
	assert.Equal(t, "true", string(body["over"]))
	tampered := key[:len(key)-3] + "AAA"
	code, _ = call(t, activate, http.MethodPost, "/store/licenses/activate", activateBody("cluster-c"), false)
	assert.Equal(t, http.StatusOK, code)
	code, _ = call(t, activate, http.MethodPost, "/store/licenses/activate", `{"key":"`+tampered+`","cluster_id":"x"}`, false)
	assert.Equal(t, http.StatusBadRequest, code, "tampered key")
	assert.Equal(t, counted["active"]+4, activationCounts(t)["active"])
	assert.Equal(t, counted["invalid"]+1, activationCounts(t)["invalid"])

	code, body = call(t, show, http.MethodGet, "/license/show?id="+created.ID.Hex(), "", true)
	require.Equal(t, http.StatusOK, code)
	var activations []model.LicenseActivation
	require.NoError(t, json.Unmarshal(body["activations"], &activations))
	require.Len(t, activations, 3)
	for _, a := range activations {
		if a.ClusterID == "cluster-a" {
			assert.Equal(t, int64(2), a.Count, "the same cluster activating twice counts once")
		}
	}

	code, _ = call(t, revoke, http.MethodPost, "/license/revoke", `{"id":"`+created.ID.Hex()+`","reason":"refund"}`, true)
	require.Equal(t, http.StatusOK, code)
	code, body = call(t, activate, http.MethodPost, "/store/licenses/activate", activateBody("cluster-a"), false)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, `"revoked"`, string(body["status"]))
	assert.Equal(t, counted["revoked"]+1, activationCounts(t)["revoked"])
	_, err := licensekey.Parse(key, license.PublicKey())
	assert.NoError(t, err, "revocation is advisory: the key still verifies")

	// Someone else's admin sees nothing.
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/license/show?id="+created.ID.Hex(), nil)
	c.Set("username", "other-admin")
	Show(c, db)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestActivateIsRateLimited(t *testing.T) {
	db := mongoTestDB(t)
	require.NoError(t, license.Setup(testSeed))
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	activate := func(c *gin.Context) { Activate(c, db, rdb) }
	var code int
	for i := 0; i <= int(activatePerIPPerHour); i++ {
		code, _ = call(t, activate, http.MethodPost, "/store/licenses/activate", `{"key":"dpl1.x.y","cluster_id":"c"}`, false)
	}
	assert.Equal(t, http.StatusTooManyRequests, code)
}
