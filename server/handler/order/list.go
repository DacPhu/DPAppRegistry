package order

import (
	"context"
	"net/http"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/model"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// List answers GET /order/list?status=: the store's orders, newest first, for the admin.
func List(c *gin.Context, database *mongo.Database) {
	filter := bson.M{}
	if status := c.Query("status"); status != "" {
		filter["status"] = status
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cursor, err := database.Collection("orders").Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(200).SetProjection(bson.M{"view_token": 0}))
	if err != nil {
		logrus.Errorf("Failed to list orders: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list orders"})
		return
	}
	defer cursor.Close(ctx)
	orders := []model.Order{}
	if err := cursor.All(ctx, &orders); err != nil {
		logrus.Errorf("Failed to decode orders: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list orders"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}
