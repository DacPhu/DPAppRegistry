package license

import (
	"context"
	"net/http"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// List answers GET /license/list?product=: the admin's licenses, newest first,
// without their keys.
func List(c *gin.Context, database *mongo.Database) {
	owner, err := utils.GetUsernameFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	filter := bson.M{"owner": owner}
	if product := c.Query("product"); product != "" {
		filter["product"] = product
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cursor, err := database.Collection("licenses").Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetProjection(bson.M{"key": 0}))
	if err != nil {
		logrus.Errorf("Failed to list licenses: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list licenses"})
		return
	}
	defer cursor.Close(ctx)
	licenses := []model.License{}
	if err := cursor.All(ctx, &licenses); err != nil {
		logrus.Errorf("Failed to decode licenses: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list licenses"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"licenses": licenses})
}
