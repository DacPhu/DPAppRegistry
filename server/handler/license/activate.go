package license

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/DacPhu/DPAppRegistry/sdk/go/licensekey"
	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const activatePerIPPerHour int64 = 30

var clusterIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type activateRequest struct {
	Key       string `json:"key" binding:"required"`
	ClusterID string `json:"cluster_id" binding:"required"`
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	Arch      string `json:"arch"`
}

// Activate answers POST /store/licenses/activate: an app reports the cluster
// it runs on. The registry records it and says how the license stands; the
// key stays valid offline either way, so this only informs the app.
func Activate(c *gin.Context, database *mongo.Database, rdb *redis.Client) {
	if !utils.EnforceRateLimits(c, rdb, []utils.RateLimit{{Key: "license:rl:activate:ip:" + utils.ClientIPKey(c), Limit: activatePerIPPerHour, Window: time.Hour}}, time.Now()) {
		return
	}
	if !license.Enabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "this registry does not issue licenses"})
		return
	}
	var req activateRequest
	if err := c.ShouldBindJSON(&req); err != nil || !clusterIDPattern.MatchString(req.ClusterID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key and a cluster_id are required"})
		return
	}
	parsed, err := licensekey.Parse(req.Key, license.PublicKey())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the key is not a license this registry issued"})
		return
	}
	id, err := primitive.ObjectIDFromHex(parsed.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the key is not a license this registry issued"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var doc model.License
	err = database.Collection("licenses").FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown license"})
		return
	}
	if err != nil {
		logrus.Errorf("Failed to load license for activation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to activate"})
		return
	}

	now := time.Now().UTC()
	_, err = database.Collection("license_activations").UpdateOne(ctx,
		bson.M{"license_id": id, "cluster_id": req.ClusterID},
		bson.M{
			"$set":         bson.M{"last_seen": now, "version": trim(req.Version), "platform": trim(req.Platform), "arch": trim(req.Arch), "owner": doc.Owner, "product": doc.Product},
			"$setOnInsert": bson.M{"_id": primitive.NewObjectID(), "first_seen": now},
			"$inc":         bson.M{"count": 1},
		}, options.Update().SetUpsert(true))
	if err != nil {
		logrus.Errorf("Failed to record activation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to activate"})
		return
	}
	active, err := database.Collection("license_activations").CountDocuments(ctx, bson.M{"license_id": id})
	if err != nil {
		logrus.Errorf("Failed to count activations: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to activate"})
		return
	}

	status := "active"
	switch {
	case doc.RevokedAt != nil:
		status = "revoked"
	case now.After(doc.ExpiresAt):
		status = "expired"
	}
	c.JSON(http.StatusOK, gin.H{
		"status":           status,
		"clusters_allowed": doc.Clusters,
		"clusters_active":  active,
		"over":             doc.Clusters > 0 && active > int64(doc.Clusters),
		"expires_at":       doc.ExpiresAt,
		"updates_until":    doc.UpdatesUntil,
	})
}

func trim(s string) string {
	if len(s) > 64 {
		return s[:64]
	}
	return s
}
