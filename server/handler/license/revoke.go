package license

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type revokeRequest struct {
	ID     string `json:"id" binding:"required"`
	Reason string `json:"reason"`
}

// Revoke answers POST /license/revoke. The key keeps verifying offline; apps
// that activate learn the license is revoked.
func Revoke(c *gin.Context, database *mongo.Database) {
	var req revokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	owner, err := utils.GetUsernameFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	id, err := primitive.ObjectIDFromHex(req.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	err = license.Revoke(ctx, database, owner, id, req.Reason)
	if errors.Is(err, license.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "license not found, or already revoked"})
		return
	}
	if err != nil {
		logrus.Errorf("Failed to revoke license: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to revoke license"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "License revoked"})
}
