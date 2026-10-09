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

// Show answers GET /license/show?id=&key=1: one license with the clusters that
// activated it, and its key when asked for.
func Show(c *gin.Context, database *mongo.Database) {
	owner, err := utils.GetUsernameFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	id, err := primitive.ObjectIDFromHex(c.Query("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	doc, err := license.Get(ctx, database, owner, id)
	if errors.Is(err, license.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "license not found"})
		return
	}
	if err != nil {
		logrus.Errorf("Failed to load license: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load license"})
		return
	}
	activations, err := license.Activations(ctx, database, id)
	if err != nil {
		logrus.Errorf("Failed to load activations: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load license"})
		return
	}
	body := gin.H{"license": doc, "activations": activations}
	if c.Query("key") == "1" {
		body["key"] = doc.Key
	}
	c.JSON(http.StatusOK, body)
}
