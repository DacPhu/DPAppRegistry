package sign

import (
	"context"
	"crypto/subtle"
	"github.com/DacPhu/DPAppRegistry/mongod"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const maxPasswordBytes = 72

func SignUp(c *gin.Context, database *mongo.Database, client *mongo.Client, rdb *redis.Client) {
	if !enforceRateLimits(c, rdb, []rateLimit{signupIPLimit(c), signupGlobalLimit}, timeNow()) {
		return
	}

	var creds model.Credentials
	if err := c.BindJSON(&creds); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" || subtle.ConstantTimeCompare([]byte(creds.SecretKey), []byte(apiKey)) != 1 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "wrong api key"})
		return
	}
	// bcrypt cannot hash more than 72 bytes, so a longer password is refused here instead of failing to hash.
	if creds.Password == "" || len(creds.Password) > maxPasswordBytes {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "password must be between 1 and 72 bytes"})
		return
	}
	ctx, ctxErr := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer ctxErr()
	// check the user credentials against the admins collection in MongoDB
	admins := database.Collection("admins")
	var result bson.M
	err := admins.FindOne(ctx, bson.M{"username": creds.Username}).Decode(&result)
	if err == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user already exists"})
		return
	}
	err = mongod.CreateUser(client, database, &creds)
	if err != nil {
		logrus.Errorf("Failed to create admin user %s: %v", creds.Username, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": "Successfully created admin user."})
}
