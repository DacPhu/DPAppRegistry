// Package license is the admin API over the licenses a registry sells, and the
// public endpoint apps activate against.
package license

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/mongo"
)

type createRequest struct {
	Product  string                `json:"product" binding:"required"`
	Plan     string                `json:"plan" binding:"required"`
	Customer model.LicenseCustomer `json:"customer" binding:"required"`
	Clusters int                   `json:"clusters"`
	// Either ExpiresAt or Years (from now) sets the term.
	ExpiresAt    *time.Time `json:"expires_at"`
	Years        int        `json:"years"`
	UpdatesUntil *time.Time `json:"updates_until"`
	ClusterIDs   []string   `json:"cluster_ids"`
	Note         string     `json:"note"`
}

// Create answers POST /license/create: an admin issues a license by hand, for a
// contract or a bank transfer confirmed outside the store. The key is returned once.
func Create(c *gin.Context, database *mongo.Database) {
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	owner, err := utils.GetUsernameFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	expires := time.Time{}
	switch {
	case req.ExpiresAt != nil:
		expires = *req.ExpiresAt
	case req.Years > 0:
		expires = time.Now().AddDate(req.Years, 0, 0)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at or years is required"})
		return
	}
	if expires.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be in the future"})
		return
	}
	updatesUntil := expires
	if req.UpdatesUntil != nil {
		updatesUntil = *req.UpdatesUntil
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	doc, err := license.Issue(ctx, database, license.IssueRequest{
		Owner: owner, Product: req.Product, Plan: req.Plan, Customer: req.Customer,
		Clusters: req.Clusters, ClusterIDs: req.ClusterIDs, ExpiresAt: expires, UpdatesUntil: updatesUntil,
		IssuedBy: owner, Note: req.Note,
	})
	if err != nil {
		if errors.Is(err, license.ErrDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if mongo.IsDuplicateKeyError(err) || isRequestError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		logrus.Errorf("Failed to issue license: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue license"})
		return
	}
	c.JSON(http.StatusCreated, withKey(doc))
}

// Every error Issue returns before touching the database is the caller's.
func isRequestError(err error) bool {
	return !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) &&
		!errors.Is(err, mongo.ErrClientDisconnected) && !mongo.IsNetworkError(err) && !mongo.IsTimeout(err)
}

// withKey renders a license with its key, which the model's JSON leaves out.
func withKey(doc *model.License) gin.H {
	return gin.H{"license": doc, "key": doc.Key}
}
