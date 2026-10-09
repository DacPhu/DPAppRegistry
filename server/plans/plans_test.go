package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/usage"

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

func plan(t *testing.T, id string) Plan {
	t.Helper()
	p, ok := Find(id)
	require.True(t, ok, id)
	return p
}

func TestCatalogMatchesThePriceList(t *testing.T) {
	hobby, starter, team, business := plan(t, "hobby"), plan(t, "starter"), plan(t, "team"), plan(t, "business")
	assert.Equal(t, Limits{Apps: 2, StorageBytes: 5 << 30, Members: 1, MAI: 1000}, hobby.Limits)
	assert.True(t, hobby.HardMAICap)
	assert.Equal(t, int64(10000), starter.Limits.MAI)
	assert.Equal(t, 2.0, starter.OverageUSD)
	assert.Equal(t, int64(40000), team.OverageVND)
	assert.Zero(t, business.Limits.Apps, "Business has no app limit")
	_, found := Find("platinum")
	assert.False(t, found)

	assert.False(t, Account{Plan: hobby}.Has(PrivateApps))
	assert.True(t, Account{Plan: starter}.Has(PrivateApps))
	assert.False(t, Account{Plan: starter}.Has(Rollouts))
	assert.True(t, Account{Plan: team}.Has(Rollouts))
	assert.False(t, Account{Plan: team}.Has(TUF))
	assert.True(t, Account{Plan: Internal}.Has(TUF))
}

func TestRequireNamesTheCheapestPlanWithTheFeature(t *testing.T) {
	err := Account{Plan: plan(t, "hobby")}.Require(PrivateApps)
	require.Error(t, err)
	assert.Equal(t, "Private apps need the Starter plan or higher; this account is on Hobby.", err.Error())
	assert.Contains(t, Account{Plan: plan(t, "starter")}.Require(Rollouts).Error(), "the Team plan or higher")
	assert.Contains(t, Account{Plan: plan(t, "team")}.Require(TUF).Error(), "TUF signing needs the Business plan or higher")
	assert.NoError(t, Account{Plan: plan(t, "team")}.Require(Reports))
}

func TestReleaseFlags(t *testing.T) {
	starter, team := Account{Plan: plan(t, "starter")}, Account{Plan: plan(t, "team")}
	for _, params := range []map[string]interface{}{
		{"rollout": 50}, {"rollout": 25.0}, {"rollout": "10"}, {"intermediate": "true"}, {"intermediate": true},
	} {
		assert.Error(t, starter.CheckReleaseFlags(params), "%v", params)
		assert.NoError(t, team.CheckReleaseFlags(params), "%v", params)
	}
	assert.NoError(t, starter.CheckReleaseFlags(map[string]interface{}{"rollout": 100, "intermediate": "false"}))
	assert.NoError(t, starter.CheckReleaseFlags(map[string]interface{}{}))
}

func TestRespondAnswers402ForLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	assert.False(t, Respond(c, nil))
	assert.True(t, Respond(c, Account{Plan: plan(t, "hobby")}.Require(Reports)))
	assert.Equal(t, http.StatusPaymentRequired, recorder.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, "plan_limit", body["code"])
	assert.Equal(t, "reports", body["limit"])
	assert.Equal(t, "hobby", body["plan"])
}

func TestEverythingIsAllowedWhilePlansAreOff(t *testing.T) {
	viper.Set("PLANS_ENABLED", false)
	account, err := Load(context.Background(), "anyone")
	require.NoError(t, err)
	assert.Equal(t, "internal", account.Plan.ID)
	assert.Equal(t, Limits{}, account.Limits)
	assert.True(t, HasFeature(context.Background(), "anyone", TUF))
}

// The rest needs Mongo for accounts, apps and team users.

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

// enabled sets up plans against the test databases with enforcement on.
func enabled(t *testing.T) (*mongo.Database, *redis.Client) {
	t.Helper()
	db := mongoTestDB(t)
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	Setup(db, rdb)
	viper.Set("PLANS_ENABLED", true)
	viper.Set("PLAN_DEFAULT", "hobby")
	t.Cleanup(func() { viper.Set("PLANS_ENABLED", false) })

	owners := bson.M{"$regex": "^plans-test-"}
	cleanup := func() {
		ctx := context.Background()
		_, _ = db.Collection("admins").DeleteMany(ctx, bson.M{"username": owners})
		_, _ = db.Collection("apps_meta").DeleteMany(ctx, bson.M{"owner": owners})
		_, _ = db.Collection("apps").DeleteMany(ctx, bson.M{"owner": owners})
		_, _ = db.Collection("team_users").DeleteMany(ctx, bson.M{"owner": owners})
		cacheMu.Lock()
		cache = map[string]cachedAccount{}
		cacheMu.Unlock()
	}
	cleanup()
	t.Cleanup(cleanup)
	return db, rdb
}

func TestLoadAppliesTheAccountsOverrides(t *testing.T) {
	db, _ := enabled(t)
	ctx := context.Background()
	_, err := db.Collection("admins").InsertMany(ctx, []interface{}{
		bson.M{"username": "plans-test-contract", "plan": "business", "plan_limits": bson.M{"mai": int64(400000), "members": 25}},
		bson.M{"username": "plans-test-noplan"},
		bson.M{"username": "plans-test-oddplan", "plan": "platinum"},
	})
	require.NoError(t, err)

	contract, err := Load(ctx, "plans-test-contract")
	require.NoError(t, err)
	assert.Equal(t, "business", contract.Plan.ID)
	assert.Equal(t, Limits{StorageBytes: 1024 << 30, Members: 25, MAI: 400000}, contract.Limits)

	for _, owner := range []string{"plans-test-noplan", "plans-test-oddplan", "plans-test-missing"} {
		account, err := Load(ctx, owner)
		require.NoError(t, err)
		assert.Equal(t, "hobby", account.Plan.ID, owner)
	}
}

func TestAppsMembersStorageAndInstalls(t *testing.T) {
	db, rdb := enabled(t)
	ctx := context.Background()
	hobby := Account{Owner: "plans-test-hobby", Plan: plan(t, "hobby"), Limits: plan(t, "hobby").Limits}
	starter := Account{Owner: "plans-test-starter", Plan: plan(t, "starter"), Limits: plan(t, "starter").Limits}

	// Apps: Hobby holds two.
	_, err := db.Collection("apps_meta").InsertMany(ctx, []interface{}{
		bson.M{"owner": hobby.Owner, "app_name": "one"},
		bson.M{"owner": hobby.Owner, "channel_name": "stable"},
	})
	require.NoError(t, err)
	assert.NoError(t, hobby.CheckNewApp(ctx), "a channel is not an app")
	_, err = db.Collection("apps_meta").InsertOne(ctx, bson.M{"owner": hobby.Owner, "app_name": "two"})
	require.NoError(t, err)
	assert.ErrorContains(t, hobby.CheckNewApp(ctx), "allows 2 apps")

	// Members: Hobby is the account alone; Starter is the account and two team users.
	assert.ErrorContains(t, hobby.CheckNewMember(ctx), "allows 1 members")
	assert.NoError(t, starter.CheckNewMember(ctx))
	_, err = db.Collection("team_users").InsertMany(ctx, []interface{}{bson.M{"owner": starter.Owner, "username": "a"}, bson.M{"owner": starter.Owner, "username": "b"}})
	require.NoError(t, err)
	assert.ErrorContains(t, starter.CheckNewMember(ctx), "allows 3 members")

	// Storage: what is stored plus what comes in must fit.
	small := starter
	small.Limits.StorageBytes = 1000
	_, err = db.Collection("apps").InsertOne(ctx, bson.M{"owner": small.Owner, "artifacts": bson.A{bson.M{"length": int64(900)}}})
	require.NoError(t, err)
	assert.NoError(t, small.CheckUpload(ctx, 100))
	err = small.CheckUpload(ctx, 101)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has 100 B of its 1000 B left")

	// Installs: past its 1,000, Hobby stops uploads; Starter only accrues overage.
	now := time.Now()
	for i := 0; i < 1100; i++ {
		usage.RecordInstall(ctx, rdb, hobby.Owner, "one", fmt.Sprintf("device-%d", i), "", now)
		usage.RecordInstall(ctx, rdb, starter.Owner, "one", fmt.Sprintf("device-%d", i), "", now)
	}
	starter.Limits.MAI = 1000
	assert.ErrorContains(t, hobby.CheckUpload(ctx, 1), "passed the Hobby plan's 1000 active installs")
	assert.NoError(t, starter.CheckUpload(ctx, 1))
}

func TestUploadGateRefusesBeforeTheBodyIsRead(t *testing.T) {
	db, _ := enabled(t)
	ctx := context.Background()
	_, err := db.Collection("admins").InsertOne(ctx, bson.M{"username": "plans-test-gate", "plan": "hobby", "plan_limits": bson.M{"storage_bytes": int64(1000)}})
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handled := false
	router.POST("/upload", func(c *gin.Context) { c.Set("username", "plans-test-gate") }, UploadGate(), func(c *gin.Context) {
		handled = true
		c.Status(http.StatusOK)
	})

	small := httptest.NewRecorder()
	router.ServeHTTP(small, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(strings.Repeat("x", 500))))
	assert.Equal(t, http.StatusOK, small.Code)
	assert.True(t, handled)

	handled = false
	big := httptest.NewRecorder()
	router.ServeHTTP(big, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(strings.Repeat("x", 1500))))
	assert.Equal(t, http.StatusPaymentRequired, big.Code)
	assert.False(t, handled, "the handler must not run")
}

func TestReleaseEditsStillWorkPastTheInstallCap(t *testing.T) {
	db, rdb := enabled(t)
	ctx := context.Background()
	_, err := db.Collection("admins").InsertOne(ctx, bson.M{"username": "plans-test-capped", "plan": "hobby"})
	require.NoError(t, err)
	for i := 0; i < 1100; i++ {
		usage.RecordInstall(ctx, rdb, "plans-test-capped", "one", fmt.Sprintf("device-%d", i), "", time.Now())
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	asCapped := func(c *gin.Context) { c.Set("username", "plans-test-capped") }
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	router.POST("/upload", asCapped, UploadGate(), ok)
	router.POST("/apps/update", asCapped, StorageGate(), ok)

	upload := httptest.NewRecorder()
	router.ServeHTTP(upload, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("x")))
	assert.Equal(t, http.StatusPaymentRequired, upload.Code, "no new uploads past the cap")

	unpublish := httptest.NewRecorder()
	router.ServeHTTP(unpublish, httptest.NewRequest(http.MethodPost, "/apps/update", strings.NewReader(`{"publish":false}`)))
	assert.Equal(t, http.StatusOK, unpublish.Code, "pulling a bad release must keep working")
}
