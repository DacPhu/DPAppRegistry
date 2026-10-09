package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LicenseCustomer is who a license was sold to.
type LicenseCustomer struct {
	Name    string `bson:"name" json:"name"`
	Email   string `bson:"email,omitempty" json:"email,omitempty"`
	Company string `bson:"company,omitempty" json:"company,omitempty"`
}

// License is a key the registry issued for one of its owner's products. Key
// is left out of JSON unless a handler adds it on purpose.
type License struct {
	ID           primitive.ObjectID  `bson:"_id" json:"id"`
	Owner        string              `bson:"owner" json:"owner"`
	Product      string              `bson:"product" json:"product"`
	Plan         string              `bson:"plan" json:"plan"`
	Customer     LicenseCustomer     `bson:"customer" json:"customer"`
	Clusters     int                 `bson:"clusters" json:"clusters"`
	ClusterIDs   []string            `bson:"cluster_ids,omitempty" json:"cluster_ids,omitempty"`
	IssuedAt     time.Time           `bson:"issued_at" json:"issued_at"`
	ExpiresAt    time.Time           `bson:"expires_at" json:"expires_at"`
	UpdatesUntil time.Time           `bson:"updates_until" json:"updates_until"`
	RevokedAt    *time.Time          `bson:"revoked_at,omitempty" json:"revoked_at,omitempty"`
	RevokeReason string              `bson:"revoke_reason,omitempty" json:"revoke_reason,omitempty"`
	Key          string              `bson:"key" json:"-"`
	KeyID        string              `bson:"key_id" json:"key_id"`
	OrderID      *primitive.ObjectID `bson:"order_id,omitempty" json:"order_id,omitempty"`
	IssuedBy     string              `bson:"issued_by" json:"issued_by"`
	Note         string              `bson:"note,omitempty" json:"note,omitempty"`
	CreatedAt    time.Time           `bson:"created_at" json:"created_at"`
}

// LicenseActivation is one cluster that reported running under a license.
type LicenseActivation struct {
	ID        primitive.ObjectID `bson:"_id" json:"-"`
	LicenseID primitive.ObjectID `bson:"license_id" json:"-"`
	Owner     string             `bson:"owner" json:"-"`
	ClusterID string             `bson:"cluster_id" json:"cluster_id"`
	Product   string             `bson:"product" json:"product"`
	Version   string             `bson:"version,omitempty" json:"version,omitempty"`
	Platform  string             `bson:"platform,omitempty" json:"platform,omitempty"`
	Arch      string             `bson:"arch,omitempty" json:"arch,omitempty"`
	FirstSeen time.Time          `bson:"first_seen" json:"first_seen"`
	LastSeen  time.Time          `bson:"last_seen" json:"last_seen"`
	Count     int64              `bson:"count" json:"count"`
}
