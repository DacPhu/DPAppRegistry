package order

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DacPhu/DPAppRegistry/sdk/go/licensekey"
	"github.com/DacPhu/DPAppRegistry/server/license"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

const testSeed = "0303030303030303030303030303030303030303030303030303030303030303"

func storeTestDB(t *testing.T) *mongo.Database {
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
	db := client.Database(parsed.Database)
	ctx := context.Background()
	cleanup := func() {
		_, _ = db.Collection("orders").DeleteMany(ctx, bson.M{"owner": "store-owner-test"})
		_, _ = db.Collection("licenses").DeleteMany(ctx, bson.M{"owner": "store-owner-test"})
		_, _ = db.Collection("webhook_events").DeleteMany(ctx, bson.M{"event_id": bson.M{"$regex": "^sepay_88"}})
	}
	cleanup()
	t.Cleanup(cleanup)
	_, _ = db.Collection("webhook_events").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "event_id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("event_id_unique")})

	require.NoError(t, license.Setup(testSeed))
	for key, value := range map[string]interface{}{
		"ORDERS_ENABLED": true, "STORE_OWNER": "store-owner-test", "STORE_URL": "https://store.test",
		"SEPAY_BANK_ACCOUNT": "0010000000355", "SEPAY_BANK_NAME": "Vietcombank", "SEPAY_ACCOUNT_HOLDER": "MITEKBRO",
		"SEPAY_WEBHOOK_SECRET": "whsec", "SEPAY_WEBHOOK_APIKEY": "", "SLACK_ENABLE": false, "SMTP_HOST": "",
	} {
		viper.Set(key, value)
	}
	t.Cleanup(func() {
		viper.Set("ORDERS_ENABLED", false)
		viper.Set("STORE_OWNER", "")
	})
	return db
}

type call struct {
	code int
	body map[string]json.RawMessage
	raw  string
}

func do(t *testing.T, handle func(*gin.Context), method, target, body string, headers map[string]string) call {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.RemoteAddr = "198.51.100.7:4000"
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	if strings.HasPrefix(target, "/store/orders/") {
		path := strings.TrimPrefix(target, "/store/orders/")
		c.Params = gin.Params{{Key: "id", Value: strings.SplitN(path, "?", 2)[0]}}
	}
	handle(c)
	var decoded map[string]json.RawMessage
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return call{recorder.Code, decoded, recorder.Body.String()}
}

func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func sepaySigned(secret, body string) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + body))
	return map[string]string{"X-SePay-Signature": "sha256=" + hex.EncodeToString(mac.Sum(nil)), "X-SePay-Timestamp": ts}
}

func TestCheckoutFromOrderToKey(t *testing.T) {
	db := storeTestDB(t)
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	create := func(c *gin.Context) { Create(c, db, rdb) }
	status := func(c *gin.Context) { Status(c, db, rdb) }
	webhook := func(c *gin.Context) { SePayWebhook(c, db) }

	r := do(t, func(c *gin.Context) { Products(c) }, http.MethodGet, "/store/products", "", nil)
	require.Equal(t, http.StatusOK, r.code)
	assert.Equal(t, "true", string(r.body["orders_enabled"]))
	assert.Contains(t, r.raw, `"vnd":4900000`)

	for name, body := range map[string]string{
		"no name":      `{"product":"dpfs","plan":"business","quantity":1,"customer":{"name":"A","email":"a@b.vn"}}`,
		"bad email":    `{"product":"dpfs","plan":"business","quantity":1,"customer":{"name":"ABC","email":"nope"}}`,
		"not for sale": `{"product":"dpfs","plan":"community","quantity":1,"customer":{"name":"ABC","email":"a@b.vn"}}`,
		"too many":     `{"product":"dpfs","plan":"business","quantity":99,"customer":{"name":"ABC","email":"a@b.vn"}}`,
	} {
		r := do(t, create, http.MethodPost, "/store/orders", body, nil)
		assert.Equal(t, http.StatusBadRequest, r.code, name)
	}

	r = do(t, create, http.MethodPost, "/store/orders", `{"product":"dpfs","plan":"business","quantity":2,"customer":{"name":"Công ty ABC","email":"IT@abc.vn","company":"ABC"},"lang":"vi"}`, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id, token, code := str(r.body["id"]), str(r.body["view_token"]), str(r.body["payment_code"])
	assert.Equal(t, `"pending"`, string(r.body["status"]))
	assert.Equal(t, "9800000", string(r.body["amount_vnd"]))
	assert.True(t, strings.HasPrefix(code, "MTB") && len(code) == 11, code)
	assert.Contains(t, str(r.body["qr_url"]), "qr.sepay.vn/img?acc=0010000000355&amount=9800000&bank=Vietcombank&des="+code)
	assert.Equal(t, "https://store.test/order/"+id+"?t="+token, str(r.body["url"]))
	assert.Equal(t, "null", string(r.body["license"]))

	r = do(t, status, http.MethodGet, "/store/orders/"+id+"?t=wrong", "", nil)
	assert.Equal(t, http.StatusNotFound, r.code)
	r = do(t, status, http.MethodGet, "/store/orders/000000000000000000000000?t="+token, "", nil)
	assert.Equal(t, http.StatusNotFound, r.code)
	r = do(t, status, http.MethodGet, "/store/orders/"+id+"?t="+token, "", nil)
	require.Equal(t, http.StatusOK, r.code)
	assert.Equal(t, `"pending"`, string(r.body["status"]))
	_, hasToken := r.body["view_token"]
	assert.False(t, hasToken, "the status never repeats the token")

	// SePay: unsigned, then underpaid, then paid.
	payload := func(txID int, amount int, content string) string {
		return `{"id":` + strconv.Itoa(txID) + `,"gateway":"Vietcombank","transactionDate":"2026-10-09 10:00:00","content":"` + content + `","transferType":"in","transferAmount":` + strconv.Itoa(amount) + `,"referenceCode":"REF"}`
	}
	body := payload(8801, 9_800_000, "ck "+code)
	r = do(t, webhook, http.MethodPost, "/webhooks/sepay", body, nil)
	assert.Equal(t, http.StatusUnauthorized, r.code)
	r = do(t, webhook, http.MethodPost, "/webhooks/sepay", body, sepaySigned("other", body))
	assert.Equal(t, http.StatusUnauthorized, r.code)

	under := payload(8802, 1_000_000, code)
	r = do(t, webhook, http.MethodPost, "/webhooks/sepay", under, sepaySigned("whsec", under))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	assert.Equal(t, `"underpaid"`, string(r.body["result"]))
	assert.Equal(t, "true", string(r.body["success"]))

	r = do(t, webhook, http.MethodPost, "/webhooks/sepay", body, sepaySigned("whsec", body))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	assert.Equal(t, `"matched"`, string(r.body["result"]))
	r = do(t, webhook, http.MethodPost, "/webhooks/sepay", body, sepaySigned("whsec", body))
	require.Equal(t, http.StatusOK, r.code)
	assert.Equal(t, `"duplicate"`, string(r.body["result"]))

	r = do(t, status, http.MethodGet, "/store/orders/"+id+"?t="+token, "", nil)
	require.Equal(t, http.StatusOK, r.code)
	assert.Equal(t, `"paid"`, string(r.body["status"]))
	var lic struct {
		Key      string `json:"key"`
		Plan     string `json:"plan"`
		Clusters int    `json:"clusters"`
	}
	require.NoError(t, json.Unmarshal(r.body["license"], &lic))
	assert.Equal(t, "business", lic.Plan)
	assert.Equal(t, 2, lic.Clusters)
	parsed, err := licensekey.Parse(lic.Key, license.PublicKey())
	require.NoError(t, err)
	assert.Equal(t, "Công ty ABC", parsed.Customer.Name)
	assert.Equal(t, "it@abc.vn", parsed.Customer.Email, "emails are stored lower-cased")

	// The garbage SePay might send on "Test Send" is acknowledged, not retried.
	r = do(t, webhook, http.MethodPost, "/webhooks/sepay", "{}", sepaySigned("whsec", "{}"))
	assert.Equal(t, http.StatusOK, r.code)
	assert.Equal(t, `"ignored"`, string(r.body["result"]))
}

func TestOrderCreationIsRateLimited(t *testing.T) {
	db := storeTestDB(t)
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	create := func(c *gin.Context) { Create(c, db, rdb) }
	var code int
	for i := 0; i <= int(createPerIPPerHour); i++ {
		code = do(t, create, http.MethodPost, "/store/orders", `{"product":"dpfs","plan":"business","customer":{"name":"ABC","email":"a@b.vn"}}`, nil).code
	}
	assert.Equal(t, http.StatusTooManyRequests, code)
}
