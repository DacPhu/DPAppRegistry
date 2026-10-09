package license

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DacPhu/DPAppRegistry/sdk/go/licensekey"
	"github.com/DacPhu/DPAppRegistry/server/model"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrNotFound is a license that does not exist or belongs to someone else.
var ErrNotFound = errors.New("license not found")

// IssueRequest is what a new license says.
type IssueRequest struct {
	Owner    string
	Product  string
	Plan     string
	Customer model.LicenseCustomer
	// Clusters is how many servers the license covers; ClusterIDs optionally
	// locks it to those clusters.
	Clusters   int
	ClusterIDs []string
	ExpiresAt  time.Time
	// UpdatesUntil defaults to ExpiresAt.
	UpdatesUntil time.Time
	OrderID      *primitive.ObjectID
	// IssuedBy is the admin's username, or "order" for a paid order.
	IssuedBy string
	Note     string
}

// Issue signs a license and keeps it in the licenses collection.
func Issue(ctx context.Context, db *mongo.Database, req IssueRequest) (*model.License, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	_, plan, ok := Find(req.Product, req.Plan)
	if !ok {
		return nil, fmt.Errorf("unknown product or plan: %s/%s", req.Product, req.Plan)
	}
	if plan.UnitVND == 0 && !plan.Contact {
		return nil, fmt.Errorf("the %s plan needs no license", plan.Name)
	}
	req.Customer.Name = strings.TrimSpace(req.Customer.Name)
	req.Customer.Email = strings.TrimSpace(req.Customer.Email)
	if req.Customer.Name == "" {
		return nil, errors.New("the customer's name is required")
	}
	if req.Clusters < 0 {
		return nil, errors.New("clusters cannot be negative")
	}
	if req.ExpiresAt.IsZero() {
		return nil, errors.New("expires_at is required")
	}
	if req.UpdatesUntil.IsZero() {
		req.UpdatesUntil = req.ExpiresAt
	}
	if req.IssuedBy == "" {
		return nil, errors.New("issued_by is required")
	}

	now := time.Now().UTC().Truncate(time.Second)
	doc := model.License{
		ID:           primitive.NewObjectID(),
		Owner:        req.Owner,
		Product:      req.Product,
		Plan:         req.Plan,
		Customer:     req.Customer,
		Clusters:     req.Clusters,
		ClusterIDs:   req.ClusterIDs,
		IssuedAt:     now,
		ExpiresAt:    req.ExpiresAt.UTC().Truncate(time.Second),
		UpdatesUntil: req.UpdatesUntil.UTC().Truncate(time.Second),
		KeyID:        KeyID(),
		OrderID:      req.OrderID,
		IssuedBy:     req.IssuedBy,
		Note:         req.Note,
		CreatedAt:    now,
	}
	key, err := licensekey.Sign(licensekey.License{
		ID:           doc.ID.Hex(),
		Product:      doc.Product,
		Plan:         doc.Plan,
		Customer:     licensekey.Customer{Name: doc.Customer.Name, Email: doc.Customer.Email},
		Clusters:     doc.Clusters,
		ClusterIDs:   doc.ClusterIDs,
		IssuedAt:     doc.IssuedAt,
		ExpiresAt:    doc.ExpiresAt,
		UpdatesUntil: doc.UpdatesUntil,
		Issuer:       Issuer(),
		KeyID:        doc.KeyID,
	}, signer)
	if err != nil {
		return nil, err
	}
	doc.Key = key
	if _, err := db.Collection("licenses").InsertOne(ctx, doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Get returns owner's license with this ID.
func Get(ctx context.Context, db *mongo.Database, owner string, id primitive.ObjectID) (*model.License, error) {
	var doc model.License
	err := db.Collection("licenses").FindOne(ctx, bson.M{"_id": id, "owner": owner}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// Revoke marks owner's license as revoked. Apps learn of it when they activate.
func Revoke(ctx context.Context, db *mongo.Database, owner string, id primitive.ObjectID, reason string) error {
	result, err := db.Collection("licenses").UpdateOne(ctx,
		bson.M{"_id": id, "owner": owner, "revoked_at": nil},
		bson.M{"$set": bson.M{"revoked_at": time.Now().UTC(), "revoke_reason": strings.TrimSpace(reason)}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// Activations lists the clusters that reported running under a license.
func Activations(ctx context.Context, db *mongo.Database, id primitive.ObjectID) ([]model.LicenseActivation, error) {
	cursor, err := db.Collection("license_activations").Find(ctx, bson.M{"license_id": id})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	activations := []model.LicenseActivation{}
	if err := cursor.All(ctx, &activations); err != nil {
		return nil, err
	}
	return activations, nil
}
