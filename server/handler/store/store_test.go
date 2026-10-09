package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	db "github.com/DacPhu/DPAppRegistry/mongod"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/plans"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

// Embedding the interface makes any repository call the test did not stub panic.
type fakeRepository struct {
	db.AppRepository
	apps     map[string][]*model.App
	releases map[string][]*model.SpecificAppWithoutIDs // by app name, newest first
	owners   []string
	channels []string
}

func (f *fakeRepository) ListApps(_ context.Context, owner string) ([]*model.App, error) {
	f.owners = append(f.owners, owner)
	return f.apps[owner], nil
}

func (f *fakeRepository) FetchLatestVersionOfApp(appName, channel string, _ context.Context, _ string) ([]*model.SpecificAppWithoutIDs, error) {
	f.channels = append(f.channels, channel)
	releases, ok := f.releases[appName]
	if !ok {
		return nil, errors.New("app not found")
	}
	return releases, nil
}

type response struct {
	Apps []app `json:"apps"`
}

func get(t *testing.T, repo *fakeRepository, target string) response {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	ListApps(c, repo, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestListAppsShowsPublicReleasedAppsOnly(t *testing.T) {
	released := primitive.NewDateTimeFromTime(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	repo := &fakeRepository{
		apps: map[string][]*model.App{"admin": {
			{AppName: "zeta", Description: "Last by name"},
			{AppName: "Alpha", Description: "Desktop editor", Logo: "https://example.test/alpha.png"},
			{AppName: "secret", Private: true},
			{AppName: "draft"},
		}},
		releases: map[string][]*model.SpecificAppWithoutIDs{
			"Alpha": {{
				Version:   "1.3.0",
				UpdatedAt: released,
				Changelog: []model.Changelog{{Version: "1.2.0", Changes: "old"}, {Version: "1.3.0", Changes: "Faster sync"}},
				Artifacts: []model.SpecificArtifactsWithoutIDs{
					{Platform: "darwin", Arch: "arm64", Package: ".dmg", Link: "https://files.test/alpha-1.3.0.dmg", Length: 1048576, Hashes: map[string]string{"sha256": "ab12", "sha512": "cd34"}},
					{Platform: "darwin", Arch: "arm64", Package: ".yml", Link: "https://files.test/latest-mac.yml", IsFeed: true},
					// Uploaded without the electron-builder updater, so not marked as a feed.
					{Platform: "darwin", Arch: "arm64", Package: ".blockmap", Link: "https://files.test/alpha-1.3.0.blockmap"},
					{Platform: "windows", Arch: "amd64", Package: ".EXE", Link: "https://files.test/alpha-1.3.0.exe"},
				},
			}, {Version: "1.2.0"}},
			"zeta":   {{Version: "0.1.0", Artifacts: []model.SpecificArtifactsWithoutIDs{{Platform: "linux", Arch: "amd64", Package: ".zip", Link: "https://files.test/zeta.zip"}}}},
			"secret": {{Version: "9.9.9", Artifacts: []model.SpecificArtifactsWithoutIDs{{Link: "https://files.test/secret"}}}},
			// "draft" has nothing published, so it has no releases at all.
		},
	}

	body := get(t, repo, "/store/apps")

	if len(body.Apps) != 2 || body.Apps[0].Name != "Alpha" || body.Apps[1].Name != "zeta" {
		t.Fatalf("want Alpha and zeta in name order, got %+v", body.Apps)
	}
	alpha := body.Apps[0]
	if alpha.Version != "1.3.0" || alpha.Channel != "stable" || alpha.Changes != "Faster sync" {
		t.Errorf("unexpected release fields: %+v", alpha)
	}
	if !alpha.UpdatedAt.Equal(released.Time()) || alpha.Logo != "https://example.test/alpha.png" {
		t.Errorf("unexpected app fields: %+v", alpha)
	}
	if len(alpha.Downloads) != 2 || alpha.Downloads[0].Package != "dmg" || alpha.Downloads[1].Package != "exe" {
		t.Fatalf("want the dmg and the exe without the update metadata, got %+v", alpha.Downloads)
	}
	if dmg := alpha.Downloads[0]; dmg.Size != 1048576 || dmg.SHA256 != "ab12" {
		t.Errorf("want the dmg's size and SHA-256, got %+v", dmg)
	}
}

func TestListAppsChoosesOwnerAndChannel(t *testing.T) {
	viper.Set("STORE_OWNER", "studio")
	defer viper.Set("STORE_OWNER", "")

	repo := &fakeRepository{apps: map[string][]*model.App{}}
	get(t, repo, "/store/apps")
	get(t, repo, "/store/apps?owner=someone&channel=beta")
	viper.Set("STORE_OWNER", "")
	get(t, repo, "/store/apps")

	want := []string{"studio", "someone", "admin"}
	for i, owner := range want {
		if repo.owners[i] != owner {
			t.Errorf("request %d listed apps of %q, want %q", i, repo.owners[i], owner)
		}
	}

	repo.apps["admin"] = []*model.App{{AppName: "app"}}
	repo.releases = map[string][]*model.SpecificAppWithoutIDs{}
	get(t, repo, "/store/apps?channel=beta")
	if last := repo.channels[len(repo.channels)-1]; last != "beta" {
		t.Errorf("channel %q, want beta", last)
	}
}

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
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client.Database(parsed.Database)
}

// An account's own page: its profile shows only on a plan with store branding, and
// an account that doesn't exist has no store.
func TestAccountStorePages(t *testing.T) {
	database := mongoTestDB(t)
	ctx := context.Background()
	admins := database.Collection("admins")
	owners := bson.M{"username": bson.M{"$regex": "^store-test-"}}
	_, _ = admins.DeleteMany(ctx, owners)
	t.Cleanup(func() { _, _ = admins.DeleteMany(ctx, owners) })
	branding := bson.M{"name": "Acme Tools", "tagline": bson.M{"en": "Tools for teams"}, "home_url": "https://acme.example"}
	_, err := admins.InsertMany(ctx, []interface{}{
		bson.M{"username": "store-test-branded", "plan": "starter", "store_profile": branding},
		bson.M{"username": "store-test-hobby", "plan": "hobby", "store_profile": branding},
	})
	if err != nil {
		t.Fatal(err)
	}
	plans.Setup(database, redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()}))
	viper.Set("PLANS_ENABLED", true)
	defer viper.Set("PLANS_ENABLED", false)

	request := func(target string) (int, map[string]json.RawMessage) {
		gin.SetMode(gin.TestMode)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, target, nil)
		ListApps(c, &fakeRepository{apps: map[string][]*model.App{}}, database)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		return recorder.Code, body
	}

	code, body := request("/store/apps?owner=store-test-branded")
	if code != http.StatusOK || string(body["account"]) != `"store-test-branded"` {
		t.Fatalf("branded: %d %s", code, body)
	}
	var shown profile
	if err := json.Unmarshal(body["profile"], &shown); err != nil || shown.Name != "Acme Tools" || shown.HomeURL != "https://acme.example" {
		t.Fatalf("branded profile = %+v, %v", shown, err)
	}

	code, body = request("/store/apps?owner=store-test-hobby")
	if code != http.StatusOK || body["profile"] != nil {
		t.Fatalf("a Hobby account's profile must not show: %d %s", code, body)
	}

	if code, _ = request("/store/apps?owner=store-test-missing"); code != http.StatusNotFound {
		t.Fatalf("missing account: %d, want 404", code)
	}
	if code, body = request("/store/apps"); code != http.StatusOK || body["account"] != nil {
		t.Fatalf("the root store names no account: %d %s", code, body)
	}
}
