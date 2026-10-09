package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

// mongoTestDB connects to Mongo when MONGODB_URL_TESTS is set and returns the database; otherwise skips the test.
// It uses the driver directly: the mongod package imports this one.
func mongoTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	mongoURL := os.Getenv("MONGODB_URL_TESTS")
	if mongoURL == "" {
		t.Skip("MONGODB_URL_TESTS not set, skipping Mongo-dependent test")
	}
	parsed, err := connstring.ParseAndValidate(mongoURL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
	})
	return client.Database(parsed.Database)
}

// Two tenants on one server: alice has a stable channel, an arm64 arch and a darwin
// platform on the manual updater; bob has only darwin, on electron-builder. Neither
// account's names may count for the other.
func TestNameChecksStayWithinOwner(t *testing.T) {
	db := mongoTestDB(t)
	meta := db.Collection("apps_meta")
	owners := bson.M{"owner": bson.M{"$in": []string{"check-alice", "check-bob"}}}
	_, _ = meta.DeleteMany(context.Background(), owners)
	t.Cleanup(func() { _, _ = meta.DeleteMany(context.Background(), owners) })
	_, err := meta.InsertMany(context.Background(), []interface{}{
		bson.M{"owner": "check-alice", "channel_name": "stable"},
		bson.M{"owner": "check-alice", "arch_id": "arm64"},
		bson.M{"owner": "check-alice", "platform_name": "darwin", "updaters": bson.A{bson.M{"type": "manual", "default": true}}},
		bson.M{"owner": "check-bob", "platform_name": "darwin", "updaters": bson.A{bson.M{"type": "electron-builder", "default": true}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	assert.NoError(t, CheckChannels("stable", "check-alice", db, c))
	assert.Error(t, CheckChannels("stable", "check-bob", db, c), "alice's channel must not exist for bob")
	assert.Error(t, CheckChannels("", "check-alice", db, c), "alice has channels, so she must name one")
	assert.NoError(t, CheckChannels("", "check-bob", db, c), "bob has none, so he may leave it empty")

	assert.NoError(t, CheckArchs("arm64", "check-alice", db, c))
	assert.Error(t, CheckArchs("arm64", "check-bob", db, c))
	arch, err := CheckArchsLatest("arm64", "check-bob", db, c)
	assert.NoError(t, err)
	assert.Empty(t, arch, "bob's checks must not resolve alice's arch")

	assert.NoError(t, CheckPlatforms("darwin", "check-bob", db, c))
	assert.Error(t, CheckPlatforms("linux", "check-bob", db, c))

	_, updater, err := CheckPlatformsLatest("darwin", "", "check-alice", db, c)
	assert.NoError(t, err)
	assert.Equal(t, "manual", updater)
	_, updater, err = CheckPlatformsLatest("darwin", "", "check-bob", db, c)
	assert.NoError(t, err)
	assert.Equal(t, "electron-builder", updater, "bob gets his own platform's default updater")
}
