package usage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

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

func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	server := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: server.Addr()})
}

func TestInstallsAreCountedPerAppAndMonth(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	october := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		// The same install checking in twice counts once.
		RecordInstall(ctx, rdb, "alice", "editor", fmt.Sprintf("device-%d", i), "", october)
		RecordInstall(ctx, rdb, "alice", "editor", fmt.Sprintf("device-%d", i), "", october)
	}
	RecordInstall(ctx, rdb, "alice", "cli", "device-0", "", october)
	RecordInstall(ctx, rdb, "alice", "cli", "", "estimate-a", october)
	RecordInstall(ctx, rdb, "alice", "cli", "", "estimate-b", october)
	RecordInstall(ctx, rdb, "bob", "editor", "device-9", "", october)
	RecordInstall(ctx, rdb, "alice", "editor", "device-7", "", october.AddDate(0, 1, 0))

	installs, err := MonthlyMAI(ctx, rdb, "alice", "2026-10")
	require.NoError(t, err)
	assert.Equal(t, int64(4), installs.Measured, "3 editor installs plus 1 cli install")
	assert.Equal(t, int64(2), installs.Estimated)
	assert.Equal(t, int64(6), installs.Total())
	assert.Equal(t, []AppInstalls{{App: "cli", Measured: 1, Estimated: 2}, {App: "editor", Measured: 3}}, installs.Apps)

	november, err := MonthlyMAI(ctx, rdb, "alice", "2026-11")
	require.NoError(t, err)
	assert.Equal(t, int64(1), november.Total())

	none, err := MonthlyMAI(ctx, rdb, "carol", "2026-10")
	require.NoError(t, err)
	assert.Zero(t, none.Total())
	assert.Empty(t, none.Apps)
}

func TestRecordInstallIgnoresChecksWithoutAnyID(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	RecordInstall(ctx, rdb, "alice", "editor", "", "", time.Now())
	RecordInstall(ctx, rdb, "", "editor", "device", "", time.Now())
	RecordInstall(ctx, nil, "alice", "editor", "device", "", time.Now())
	installs, err := MonthlyMAI(ctx, rdb, "alice", Month(time.Now()))
	require.NoError(t, err)
	assert.Zero(t, installs.Total())
}

func TestEstimatedIDDependsOnWhoAskedForWhichBuild(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ask := func(ip, agent string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/checkVersion", nil)
		c.Request.RemoteAddr = ip + ":51234"
		c.Request.Header.Set("User-Agent", agent)
		return c
	}
	a := EstimatedID(ask("203.0.113.5", "app/1.0"), "darwin", "arm64")
	assert.Len(t, a, 32)
	assert.Equal(t, a, EstimatedID(ask("203.0.113.5", "app/1.0"), "darwin", "arm64"))
	assert.NotEqual(t, a, EstimatedID(ask("203.0.113.6", "app/1.0"), "darwin", "arm64"))
	assert.NotEqual(t, a, EstimatedID(ask("203.0.113.5", "app/1.0"), "darwin", "amd64"))
}

func TestFormatBytes(t *testing.T) {
	assert.Equal(t, "512 B", FormatBytes(512))
	assert.Equal(t, "1.5 KB", FormatBytes(1536))
	assert.Equal(t, "103 MB", FormatBytes(108460712))
	assert.Equal(t, "50 GB", FormatBytes(50<<30))
}

// mongoTestDB connects to Mongo when MONGODB_URL_TESTS is set and returns the database; otherwise skips the test.
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

func TestStorageAndSnapshots(t *testing.T) {
	ctx := context.Background()
	db := mongoTestDB(t)
	rdb := testRedis(t)
	owners := bson.M{"$in": []string{"usage-alice", "usage-bob"}}
	cleanup := func() {
		_, _ = db.Collection("apps").DeleteMany(ctx, bson.M{"owner": owners})
		_, _ = db.Collection("admins").DeleteMany(ctx, bson.M{"username": owners})
		_, _ = db.Collection("usage").DeleteMany(ctx, bson.M{"owner": owners})
	}
	cleanup()
	t.Cleanup(cleanup)

	_, err := db.Collection("apps").InsertMany(ctx, []interface{}{
		bson.M{"owner": "usage-alice", "version": "1.0.0", "artifacts": bson.A{bson.M{"length": int64(100)}, bson.M{"length": int64(250)}}},
		bson.M{"owner": "usage-alice", "version": "1.1.0", "artifacts": bson.A{bson.M{"length": int64(50)}, bson.M{"link": "older upload without a length"}}},
		bson.M{"owner": "usage-bob", "version": "2.0.0", "artifacts": bson.A{bson.M{"length": int64(7)}}},
	})
	require.NoError(t, err)
	_, err = db.Collection("admins").InsertMany(ctx, []interface{}{bson.M{"username": "usage-alice"}, bson.M{"username": "usage-bob"}})
	require.NoError(t, err)

	used, err := StorageBytes(ctx, db, "usage-alice")
	require.NoError(t, err)
	assert.Equal(t, int64(400), used)
	empty, err := StorageBytes(ctx, db, "usage-nobody")
	require.NoError(t, err)
	assert.Zero(t, empty)

	now := time.Now()
	RecordInstall(ctx, rdb, "usage-alice", "editor", "device-1", "", now)
	_, err = TakeSnapshots(ctx, db, rdb, now)
	require.NoError(t, err)
	// A second run updates the month's document instead of adding one.
	_, err = TakeSnapshots(ctx, db, rdb, now)
	require.NoError(t, err)

	var saved Snapshot
	require.NoError(t, db.Collection("usage").FindOne(ctx, bson.M{"owner": "usage-alice", "month": Month(now)}).Decode(&saved))
	assert.Equal(t, int64(400), saved.StorageBytes)
	assert.Equal(t, int64(1), saved.Installs.Total())
	documents, err := db.Collection("usage").CountDocuments(ctx, bson.M{"owner": "usage-alice"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), documents)
}
