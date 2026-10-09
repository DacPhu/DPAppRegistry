package license

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DacPhu/DPAppRegistry/sdk/go/licensekey"
	"github.com/DacPhu/DPAppRegistry/server/model"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

const testSeed = "0101010101010101010101010101010101010101010101010101010101010101"

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

func TestSetupAndKeys(t *testing.T) {
	require.NoError(t, Setup(""))
	assert.False(t, Enabled())
	assert.Empty(t, PublicKeyHex())
	assert.Error(t, Setup("not-hex"))
	require.NoError(t, Setup(testSeed))
	assert.True(t, Enabled())
	assert.Equal(t, "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c", PublicKeyHex())
	assert.Equal(t, "34750f98", KeyID())

	viper.Set("STORE_URL", "https://store.mitekbro.com")
	assert.Equal(t, "store.mitekbro.com", Issuer())
	viper.Set("STORE_URL", "")
	assert.Equal(t, "dpappregistry", Issuer())
}

func TestCatalog(t *testing.T) {
	_, business, ok := Find("dpfs", "business")
	require.True(t, ok)
	assert.Equal(t, int64(4_900_000), business.UnitVND)
	assert.Equal(t, int64(9_800_000), Amount(business, 2))
	assert.Equal(t, int64(4_900_000), Amount(business, 0), "quantity is at least one")
	_, enterprise, _ := Find("dpfs", "enterprise")
	assert.True(t, enterprise.Contact)
	assert.False(t, enterprise.Purchasable)
	_, _, ok = Find("dpfs", "platinum")
	assert.False(t, ok)
}

func TestIssueSignsAKeyTheSDKReads(t *testing.T) {
	db := mongoTestDB(t)
	ctx := context.Background()
	cleanup := func() { _, _ = db.Collection("licenses").DeleteMany(ctx, bson.M{"owner": "license-test"}) }
	cleanup()
	t.Cleanup(cleanup)

	require.NoError(t, Setup(""))
	_, err := Issue(ctx, db, IssueRequest{Owner: "license-test", Product: "dpfs", Plan: "business", Customer: model.LicenseCustomer{Name: "ABC"}, ExpiresAt: time.Now().AddDate(1, 0, 0), IssuedBy: "test"})
	assert.ErrorIs(t, err, ErrDisabled)

	require.NoError(t, Setup(testSeed))
	for name, req := range map[string]IssueRequest{
		"unknown plan":  {Owner: "license-test", Product: "dpfs", Plan: "platinum", Customer: model.LicenseCustomer{Name: "ABC"}, ExpiresAt: time.Now().AddDate(1, 0, 0), IssuedBy: "test"},
		"free plan":     {Owner: "license-test", Product: "dpfs", Plan: "community", Customer: model.LicenseCustomer{Name: "ABC"}, ExpiresAt: time.Now().AddDate(1, 0, 0), IssuedBy: "test"},
		"no customer":   {Owner: "license-test", Product: "dpfs", Plan: "business", Customer: model.LicenseCustomer{Name: "  "}, ExpiresAt: time.Now().AddDate(1, 0, 0), IssuedBy: "test"},
		"no expiry":     {Owner: "license-test", Product: "dpfs", Plan: "business", Customer: model.LicenseCustomer{Name: "ABC"}, IssuedBy: "test"},
		"no issued_by":  {Owner: "license-test", Product: "dpfs", Plan: "business", Customer: model.LicenseCustomer{Name: "ABC"}, ExpiresAt: time.Now().AddDate(1, 0, 0)},
		"neg. clusters": {Owner: "license-test", Product: "dpfs", Plan: "business", Customer: model.LicenseCustomer{Name: "ABC"}, Clusters: -1, ExpiresAt: time.Now().AddDate(1, 0, 0), IssuedBy: "test"},
	} {
		_, err := Issue(ctx, db, req)
		assert.Error(t, err, name)
	}

	expires := time.Date(2027, 10, 9, 0, 0, 0, 0, time.UTC)
	doc, err := Issue(ctx, db, IssueRequest{
		Owner: "license-test", Product: "dpfs", Plan: "business",
		Customer: model.LicenseCustomer{Name: " Công ty ABC ", Email: "it@abc.vn", Company: "ABC"},
		Clusters: 2, ClusterIDs: []string{"c1"}, ExpiresAt: expires, IssuedBy: "operator", Note: "contract 12",
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(doc.Key, "dpl1."))
	assert.Equal(t, "Công ty ABC", doc.Customer.Name)
	assert.Equal(t, expires, doc.UpdatesUntil, "updates end with the term unless said otherwise")

	parsed, err := licensekey.Parse(doc.Key, PublicKey())
	require.NoError(t, err)
	assert.Equal(t, doc.ID.Hex(), parsed.ID)
	assert.Equal(t, "business", parsed.Plan)
	assert.Equal(t, 2, parsed.Clusters)
	assert.Equal(t, []string{"c1"}, parsed.ClusterIDs)
	assert.True(t, parsed.ExpiresAt.Equal(expires))
	assert.Equal(t, KeyID(), parsed.KeyID)
	assert.NoError(t, parsed.Valid(time.Now(), "c1"))
	assert.ErrorIs(t, parsed.Valid(time.Now(), "c2"), licensekey.ErrWrongCluster)

	stored, err := Get(ctx, db, "license-test", doc.ID)
	require.NoError(t, err)
	assert.Equal(t, doc.Key, stored.Key)
	_, err = Get(ctx, db, "someone-else", doc.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, Revoke(ctx, db, "license-test", doc.ID, "refunded"))
	assert.ErrorIs(t, Revoke(ctx, db, "license-test", doc.ID, "again"), ErrNotFound, "revoking twice")
	stored, err = Get(ctx, db, "license-test", doc.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.RevokedAt)
	assert.Equal(t, "refunded", stored.RevokeReason)
	_, err = licensekey.Parse(doc.Key, PublicKey())
	assert.NoError(t, err, "a revoked key still verifies offline; activation reports it")
	assert.True(t, errors.Is(err, nil))
}
