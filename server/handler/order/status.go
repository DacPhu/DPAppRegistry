package order

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Status answers GET /store/orders/:id?t=<view token>: the order as the buyer
// sees it, with the license once paid. A wrong id or token is a 404 either way.
func Status(c *gin.Context, database *mongo.Database, rdb *redis.Client) {
	if !utils.EnforceRateLimits(c, rdb, []utils.RateLimit{{Key: "order:rl:status:ip:" + utils.ClientIPKey(c), Limit: statusPerIPPerMinute, Window: time.Minute}}, time.Now()) {
		return
	}
	c.Header("Cache-Control", "no-store")
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	order, err := license.FindOrderForView(ctx, database, id, c.Query("t"))
	if errors.Is(err, license.ErrOrderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if err != nil {
		logrus.Errorf("Failed to load order: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load the order"})
		return
	}
	var lic *model.License
	if order.LicenseID != nil {
		var doc model.License
		if err := database.Collection("licenses").FindOne(ctx, bson.M{"_id": *order.LicenseID}).Decode(&doc); err == nil {
			lic = &doc
		} else {
			logrus.Errorf("Order %s: loading its license: %v", order.ID.Hex(), err)
		}
	}
	c.JSON(http.StatusOK, view(order, lic, time.Now()))
}
