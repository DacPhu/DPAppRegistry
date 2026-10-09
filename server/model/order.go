package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// OrderPayment is the bank transfer that paid an order.
type OrderPayment struct {
	Provider        string `bson:"provider" json:"provider"`
	TransactionID   int64  `bson:"transaction_id,omitempty" json:"transaction_id,omitempty"`
	ReferenceCode   string `bson:"reference_code,omitempty" json:"reference_code,omitempty"`
	TransactionDate string `bson:"transaction_date,omitempty" json:"transaction_date,omitempty"`
	Gateway         string `bson:"gateway,omitempty" json:"gateway,omitempty"`
	Amount          int64  `bson:"amount" json:"amount"`
}

// Order is a purchase started on the store. ViewToken lets whoever started it
// open its page; it is never listed.
type Order struct {
	ID          primitive.ObjectID  `bson:"_id" json:"id"`
	Owner       string              `bson:"owner" json:"owner"`
	Product     string              `bson:"product" json:"product"`
	Plan        string              `bson:"plan" json:"plan"`
	Quantity    int                 `bson:"quantity" json:"quantity"`
	UnitVND     int64               `bson:"unit_vnd" json:"unit_vnd"`
	AmountVND   int64               `bson:"amount_vnd" json:"amount_vnd"`
	PaymentCode string              `bson:"payment_code" json:"payment_code"`
	Status      string              `bson:"status" json:"status"`
	Customer    LicenseCustomer     `bson:"customer" json:"customer"`
	Lang        string              `bson:"lang" json:"lang"`
	LicenseID   *primitive.ObjectID `bson:"license_id,omitempty" json:"license_id,omitempty"`
	Payment     *OrderPayment       `bson:"payment,omitempty" json:"payment,omitempty"`
	ViewToken   string              `bson:"view_token" json:"-"`
	CreatedAt   time.Time           `bson:"created_at" json:"created_at"`
	ExpiresAt   time.Time           `bson:"expires_at" json:"expires_at"`
	PaidAt      *time.Time          `bson:"paid_at,omitempty" json:"paid_at,omitempty"`
	// FulfilmentClaimedAt marks the one process issuing the license.
	FulfilmentClaimedAt *time.Time `bson:"fulfilment_claimed_at,omitempty" json:"-"`
	FulfilledAt         *time.Time `bson:"fulfilled_at,omitempty" json:"fulfilled_at,omitempty"`
	EmailSent           bool       `bson:"email_sent" json:"email_sent"`
}

// WebhookEvent is one delivery from a payment provider, kept so a retry of
// the same transaction is handled once.
type WebhookEvent struct {
	ID         primitive.ObjectID  `bson:"_id"`
	EventID    string              `bson:"event_id"`
	Provider   string              `bson:"provider"`
	Code       string              `bson:"code,omitempty"`
	Amount     int64               `bson:"amount"`
	Outcome    string              `bson:"outcome"`
	OrderID    *primitive.ObjectID `bson:"order_id,omitempty"`
	ReceivedAt time.Time           `bson:"received_at"`
	HandledAt  *time.Time          `bson:"handled_at,omitempty"`
}
