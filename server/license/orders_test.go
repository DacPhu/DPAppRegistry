package license

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/sepay"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func uniqueIndex(name string) *options.IndexOptions {
	return options.Index().SetUnique(true).SetName(name)
}

func ordersTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	db := mongoTestDB(t)
	ctx := context.Background()
	cleanup := func() {
		_, _ = db.Collection("orders").DeleteMany(ctx, bson.M{"owner": "orders-test"})
		_, _ = db.Collection("licenses").DeleteMany(ctx, bson.M{"owner": "orders-test"})
		_, _ = db.Collection("webhook_events").DeleteMany(ctx, bson.M{"event_id": bson.M{"$regex": "^sepay_99"}})
	}
	cleanup()
	t.Cleanup(cleanup)
	// The unique indexes the migration makes, so duplicates behave as in production.
	_, _ = db.Collection("orders").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "payment_code", Value: 1}}, Options: uniqueIndex("payment_code_unique")})
	_, _ = db.Collection("webhook_events").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "event_id", Value: 1}}, Options: uniqueIndex("event_id_unique")})
	require.NoError(t, Setup(testSeed))
	viper.Set("SLACK_ENABLE", false)
	viper.Set("SMTP_HOST", "")
	return db
}

func TestPaymentCodesAndTokens(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := NewPaymentCode()
		require.NoError(t, err)
		assert.Equal(t, code, sepay.ExtractCode("tt "+code+" abc"), code)
		assert.False(t, seen[code], "codes repeat")
		seen[code] = true
	}
	token, err := NewViewToken()
	require.NoError(t, err)
	assert.Len(t, token, 48)
}

func TestCreateOrderAndFulfil(t *testing.T) {
	db := ordersTestDB(t)
	ctx := context.Background()

	_, err := CreateOrder(ctx, db, OrderRequest{Owner: "orders-test", Product: "dpfs", Plan: "community", Quantity: 1})
	assert.ErrorIs(t, err, ErrNotForSale)
	_, err = CreateOrder(ctx, db, OrderRequest{Owner: "orders-test", Product: "dpfs", Plan: "enterprise", Quantity: 1})
	assert.ErrorIs(t, err, ErrNotForSale)
	_, err = CreateOrder(ctx, db, OrderRequest{Owner: "orders-test", Product: "dpfs", Plan: "business", Quantity: 21})
	assert.ErrorContains(t, err, "quantity")

	order, err := CreateOrder(ctx, db, OrderRequest{Owner: "orders-test", Product: "dpfs", Plan: "business", Quantity: 2,
		Customer: model.LicenseCustomer{Name: "ABC", Email: "it@abc.vn"}, Lang: "vi"})
	require.NoError(t, err)
	assert.Equal(t, int64(9_800_000), order.AmountVND)
	assert.Equal(t, OrderPending, order.Status)
	assert.Equal(t, OrderExpired, DisplayStatus(order, order.ExpiresAt.Add(time.Minute)))

	_, err = FindOrderForView(ctx, db, order.ID, "wrong")
	assert.ErrorIs(t, err, ErrOrderNotFound)
	_, err = FindOrderForView(ctx, db, order.ID, "")
	assert.ErrorIs(t, err, ErrOrderNotFound)
	seen, err := FindOrderForView(ctx, db, order.ID, order.ViewToken)
	require.NoError(t, err)
	assert.Equal(t, order.PaymentCode, seen.PaymentCode)
	byCode, err := FindOrderByCode(ctx, db, order.PaymentCode)
	require.NoError(t, err)
	assert.Equal(t, order.ID, byCode.ID)

	payment := &model.OrderPayment{Provider: "sepay", TransactionID: 9901, Amount: 9_800_000}
	paid, lic, err := Fulfil(ctx, db, order.ID, payment)
	require.NoError(t, err)
	require.NotNil(t, lic)
	assert.Equal(t, OrderPaid, paid.Status)
	assert.Equal(t, &lic.ID, paid.LicenseID)
	assert.Equal(t, 2, lic.Clusters)
	assert.Equal(t, "order", lic.IssuedBy)
	assert.Equal(t, order.ID, *lic.OrderID)
	assert.WithinDuration(t, time.Now().AddDate(0, 0, 365), lic.ExpiresAt, time.Minute)

	// Fulfilling again returns the same license.
	again, lic2, err := Fulfil(ctx, db, order.ID, payment)
	require.NoError(t, err)
	assert.Equal(t, lic.ID, lic2.ID)
	assert.Equal(t, lic.Key, lic2.Key)
	assert.Equal(t, paid.ID, again.ID)
	count, _ := db.Collection("licenses").CountDocuments(ctx, bson.M{"order_id": order.ID})
	assert.Equal(t, int64(1), count)
}

func TestFulfilRepairsAPaidOrderWithoutALicense(t *testing.T) {
	db := ordersTestDB(t)
	ctx := context.Background()
	order, err := CreateOrder(ctx, db, OrderRequest{Owner: "orders-test", Product: "dpfs", Plan: "business", Quantity: 1, Customer: model.LicenseCustomer{Name: "ABC"}})
	require.NoError(t, err)
	// A crash after marking paid, with a stale claim.
	_, err = db.Collection("orders").UpdateOne(ctx, bson.M{"_id": order.ID}, bson.M{"$set": bson.M{"status": OrderPaid, "paid_at": time.Now().Add(-time.Hour), "fulfilment_claimed_at": time.Now().Add(-time.Hour)}})
	require.NoError(t, err)
	paid, lic, err := Fulfil(ctx, db, order.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, lic)
	assert.Equal(t, &lic.ID, paid.LicenseID)

	// A fresh claim held by another process is left alone.
	other, err := CreateOrder(ctx, db, OrderRequest{Owner: "orders-test", Product: "dpfs", Plan: "business", Quantity: 1, Customer: model.LicenseCustomer{Name: "ABC"}})
	require.NoError(t, err)
	_, err = db.Collection("orders").UpdateOne(ctx, bson.M{"_id": other.ID}, bson.M{"$set": bson.M{"status": OrderPaid, "paid_at": time.Now(), "fulfilment_claimed_at": time.Now()}})
	require.NoError(t, err)
	_, lic, err = Fulfil(ctx, db, other.ID, nil)
	assert.ErrorContains(t, err, "being fulfilled")
	assert.Nil(t, lic)
}

func TestProcessTransaction(t *testing.T) {
	db := ordersTestDB(t)
	ctx := context.Background()
	order, err := CreateOrder(ctx, db, OrderRequest{Owner: "orders-test", Product: "dpfs", Plan: "business", Quantity: 1, Customer: model.LicenseCustomer{Name: "ABC", Email: "it@abc.vn"}})
	require.NoError(t, err)
	tx := func(id int64, content string, amount int64, kind string) sepay.Transaction {
		return sepay.Transaction{ID: sepay.Number(id), Content: content, TransferAmount: sepay.Number(amount), TransferType: kind, Gateway: "VCB"}
	}

	out, err := ProcessTransaction(ctx, db, tx(9910, order.PaymentCode, 4_900_000, "out"), false)
	require.NoError(t, err)
	assert.Equal(t, ResultIgnoredOut, out.Result)
	out, err = ProcessTransaction(ctx, db, tx(9911, "no code here", 4_900_000, "in"), false)
	require.NoError(t, err)
	assert.Equal(t, ResultNoCode, out.Result)
	out, err = ProcessTransaction(ctx, db, tx(9912, "MTBZZZZZZZZ", 4_900_000, "in"), false)
	require.NoError(t, err)
	assert.Equal(t, ResultUnknownCode, out.Result)
	out, err = ProcessTransaction(ctx, db, tx(9913, "ck "+order.PaymentCode, 4_000_000, "in"), false)
	require.NoError(t, err)
	assert.Equal(t, ResultUnderpaid, out.Result)
	assert.Nil(t, out.License)

	dry, err := ProcessTransaction(ctx, db, tx(9914, order.PaymentCode, 4_900_000, "in"), true)
	require.NoError(t, err)
	assert.Equal(t, ResultMatched, dry.Result)
	assert.Nil(t, dry.License, "a dry run issues nothing")
	events, _ := db.Collection("webhook_events").CountDocuments(ctx, bson.M{"event_id": "sepay_9914"})
	assert.Zero(t, events, "a dry run records nothing")

	// The same transfer delivered three times at once issues one license.
	var wg sync.WaitGroup
	results := make([]Outcome, 3)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = ProcessTransaction(ctx, db, tx(9914, "chuyen tien "+order.PaymentCode+" cam on", 5_000_000, "in"), false)
		}(i)
	}
	wg.Wait()
	matched, duplicates := 0, 0
	for _, r := range results {
		switch r.Result {
		case ResultMatched:
			matched++
			require.NotNil(t, r.License)
		case ResultDuplicate:
			duplicates++
		}
	}
	assert.Equal(t, 1, matched)
	assert.Equal(t, 2, duplicates)
	licenses, _ := db.Collection("licenses").CountDocuments(ctx, bson.M{"order_id": order.ID})
	assert.Equal(t, int64(1), licenses)

	// A second, different transfer for the same code is matched to the paid order and gets the same license.
	again, err := ProcessTransaction(ctx, db, tx(9915, order.PaymentCode, 4_900_000, "in"), false)
	require.NoError(t, err)
	assert.Equal(t, ResultMatched, again.Result)
	licenses, _ = db.Collection("licenses").CountDocuments(ctx, bson.M{"order_id": order.ID})
	assert.Equal(t, int64(1), licenses)
	var event model.WebhookEvent
	require.NoError(t, db.Collection("webhook_events").FindOne(ctx, bson.M{"event_id": "sepay_9915"}).Decode(&event))
	assert.Equal(t, ResultMatched, event.Outcome)
	assert.Equal(t, order.ID, *event.OrderID)
}
