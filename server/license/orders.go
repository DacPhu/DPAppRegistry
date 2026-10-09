package license

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/sepay"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	OrderPending   = "pending"
	OrderPaid      = "paid"
	OrderCancelled = "cancelled"
	// OrderExpired is what a pending order looks like after OrderTTL; a late
	// transfer still pays it.
	OrderExpired = "expired"

	OrderTTL = 24 * time.Hour
	// A fulfilment claim older than this is taken over: the process died.
	fulfilmentStale = 10 * time.Minute
)

var (
	ErrOrderNotFound = errors.New("order not found")
	ErrNotForSale    = errors.New("this plan is not sold on the store")
)

// OrderRequest is what the store asks for.
type OrderRequest struct {
	Owner    string
	Product  string
	Plan     string
	Quantity int
	Customer model.LicenseCustomer
	Lang     string
}

// NewPaymentCode makes the code the buyer puts in the transfer's content.
func NewPaymentCode() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	code := []byte(sepay.PaymentCodePrefix)
	for _, b := range random {
		code = append(code, sepay.PaymentCodeAlphabet[int(b)%len(sepay.PaymentCodeAlphabet)])
	}
	return string(code), nil
}

// NewViewToken makes the secret that opens an order's page.
func NewViewToken() (string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

// CreateOrder records a purchase and the code to pay it with.
func CreateOrder(ctx context.Context, db *mongo.Database, req OrderRequest) (*model.Order, error) {
	_, plan, ok := Find(req.Product, req.Plan)
	if !ok || !plan.Purchasable {
		return nil, ErrNotForSale
	}
	if req.Quantity < 1 || (plan.MaxQuantity > 0 && req.Quantity > plan.MaxQuantity) {
		return nil, fmt.Errorf("quantity must be between 1 and %d", plan.MaxQuantity)
	}
	token, err := NewViewToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	order := model.Order{
		ID: primitive.NewObjectID(), Owner: req.Owner, Product: req.Product, Plan: req.Plan, Quantity: req.Quantity,
		UnitVND: plan.UnitVND, AmountVND: Amount(plan, req.Quantity), Status: OrderPending, Customer: req.Customer,
		Lang: req.Lang, ViewToken: token, CreatedAt: now, ExpiresAt: now.Add(OrderTTL),
	}
	// Codes are random enough that a collision is a once-in-a-lifetime event;
	// the unique index catches it and one retry makes another.
	for attempt := 0; attempt < 2; attempt++ {
		if order.PaymentCode, err = NewPaymentCode(); err != nil {
			return nil, err
		}
		_, err = db.Collection("orders").InsertOne(ctx, order)
		if err == nil {
			return &order, nil
		}
		if !mongo.IsDuplicateKeyError(err) {
			return nil, err
		}
	}
	return nil, err
}

// GetOrder loads an order by ID.
func GetOrder(ctx context.Context, db *mongo.Database, id primitive.ObjectID) (*model.Order, error) {
	var order model.Order
	err := db.Collection("orders").FindOne(ctx, bson.M{"_id": id}).Decode(&order)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// FindOrderForView loads an order for whoever holds its view token; a wrong
// token is the same as no order.
func FindOrderForView(ctx context.Context, db *mongo.Database, id primitive.ObjectID, token string) (*model.Order, error) {
	order, err := GetOrder(ctx, db, id)
	if err != nil {
		return nil, err
	}
	if token == "" || subtle.ConstantTimeCompare([]byte(order.ViewToken), []byte(token)) != 1 {
		return nil, ErrOrderNotFound
	}
	return order, nil
}

// FindOrderByCode loads the order a payment code belongs to.
func FindOrderByCode(ctx context.Context, db *mongo.Database, code string) (*model.Order, error) {
	var order model.Order
	err := db.Collection("orders").FindOne(ctx, bson.M{"payment_code": strings.ToUpper(code)}).Decode(&order)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// DisplayStatus is the status the buyer sees: a pending order past its time is expired.
func DisplayStatus(order *model.Order, now time.Time) string {
	if order.Status == OrderPending && now.After(order.ExpiresAt) {
		return OrderExpired
	}
	return order.Status
}

// Fulfil marks the order paid and issues its license, once: a second call for
// an order already fulfilled returns the same license, and two processes
// fulfilling at the same time issue one. An order already paid but never
// fulfilled (a crash between the two steps) is finished here too.
func Fulfil(ctx context.Context, db *mongo.Database, id primitive.ObjectID, payment *model.OrderPayment) (*model.Order, *model.License, error) {
	orders := db.Collection("orders")
	now := time.Now().UTC().Truncate(time.Second)
	after := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var order model.Order
	err := orders.FindOneAndUpdate(ctx, bson.M{"_id": id, "status": OrderPending},
		bson.M{"$set": bson.M{"status": OrderPaid, "paid_at": now, "payment": payment, "fulfilment_claimed_at": now}}, after).Decode(&order)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Not pending: paid already, or cancelled.
		existing, err := GetOrder(ctx, db, id)
		if err != nil {
			return nil, nil, err
		}
		if existing.Status != OrderPaid {
			return existing, nil, fmt.Errorf("order %s is %s", id.Hex(), existing.Status)
		}
		if existing.LicenseID != nil {
			lic, err := getLicense(ctx, db, *existing.LicenseID)
			return existing, lic, err
		}
		// Paid without a license: claim the fulfilment unless someone holds it.
		err = orders.FindOneAndUpdate(ctx, bson.M{"_id": id, "status": OrderPaid, "license_id": nil,
			"$or": bson.A{bson.M{"fulfilment_claimed_at": nil}, bson.M{"fulfilment_claimed_at": bson.M{"$lt": now.Add(-fulfilmentStale)}}}},
			bson.M{"$set": bson.M{"fulfilment_claimed_at": now}}, after).Decode(&order)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return existing, nil, fmt.Errorf("order %s is being fulfilled", id.Hex())
		}
		if err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	}

	_, plan, ok := Find(order.Product, order.Plan)
	if !ok {
		return &order, nil, fmt.Errorf("order %s names an unknown plan %s/%s", id.Hex(), order.Product, order.Plan)
	}
	paidAt := now
	if order.PaidAt != nil {
		paidAt = *order.PaidAt
	}
	expires := paidAt.AddDate(0, 0, plan.PeriodDays)
	lic, err := Issue(ctx, db, IssueRequest{
		Owner: order.Owner, Product: order.Product, Plan: order.Plan, Customer: order.Customer,
		Clusters: order.Quantity, ExpiresAt: expires, UpdatesUntil: expires, OrderID: &order.ID, IssuedBy: "order",
	})
	if err != nil {
		// Release the claim so the next attempt can finish the job.
		_, _ = orders.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$unset": bson.M{"fulfilment_claimed_at": ""}})
		return &order, nil, err
	}
	if _, err := orders.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"license_id": lic.ID, "fulfilled_at": now}}); err != nil {
		return &order, lic, err
	}
	order.LicenseID = &lic.ID
	order.FulfilledAt = &now
	return &order, lic, nil
}

func getLicense(ctx context.Context, db *mongo.Database, id primitive.ObjectID) (*model.License, error) {
	var lic model.License
	if err := db.Collection("licenses").FindOne(ctx, bson.M{"_id": id}).Decode(&lic); err != nil {
		return nil, err
	}
	return &lic, nil
}

// RecordWebhookEvent remembers a delivery before it is acted on and reports
// whether it is new. A delivery whose handling failed counts as new again, so
// the provider's retry finishes the work.
func RecordWebhookEvent(ctx context.Context, db *mongo.Database, event model.WebhookEvent) (bool, error) {
	event.ID = primitive.NewObjectID()
	event.Outcome = "received"
	event.ReceivedAt = time.Now().UTC()
	_, err := db.Collection("webhook_events").InsertOne(ctx, event)
	if err == nil {
		return true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return false, err
	}
	result, err := db.Collection("webhook_events").UpdateOne(ctx, bson.M{"event_id": event.EventID, "outcome": "failed"},
		bson.M{"$set": bson.M{"outcome": "received", "received_at": event.ReceivedAt}})
	if err != nil {
		return false, err
	}
	return result.ModifiedCount == 1, nil
}

// FinishWebhookEvent records what a delivery led to.
func FinishWebhookEvent(ctx context.Context, db *mongo.Database, eventID, outcome string, orderID *primitive.ObjectID) {
	now := time.Now().UTC()
	update := bson.M{"outcome": outcome, "handled_at": now}
	if orderID != nil {
		update["order_id"] = *orderID
	}
	_, _ = db.Collection("webhook_events").UpdateOne(ctx, bson.M{"event_id": eventID}, bson.M{"$set": update})
}
