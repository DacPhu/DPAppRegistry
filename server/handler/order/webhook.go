package order

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/sepay"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/mongo"
)

const maxWebhookBody = 64 << 10

// SePayWebhook answers POST /webhooks/sepay, one call per bank transaction.
// SePay wants 200 with {"success": true} within 30 seconds and retries
// anything else, so only a failure worth retrying (the database) is not 200.
func SePayWebhook(c *gin.Context, database *mongo.Database) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBody))
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "error": "body too large"})
		return
	}
	if err := sepay.Verify(c.Request, body, sepay.FromViper(), time.Now()); err != nil {
		if errors.Is(err, sepay.ErrNotConfigured) {
			logrus.Error("A SePay webhook arrived but no SEPAY_WEBHOOK_SECRET or SEPAY_WEBHOOK_APIKEY is set")
		} else {
			logrus.Warnf("SePay webhook rejected from %s: %v", c.ClientIP(), err)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return
	}
	var tx sepay.Transaction
	if err := json.Unmarshal(body, &tx); err != nil || tx.ID == 0 {
		// Nothing a retry would fix.
		logrus.Warnf("SePay webhook with an unreadable body: %v", err)
		c.JSON(http.StatusOK, gin.H{"success": true, "result": "ignored"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	outcome, err := license.ProcessTransaction(ctx, database, tx, false)
	if err != nil {
		logrus.Errorf("SePay transaction %d (%s): %v", int64(tx.ID), outcome.Code, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "try again"})
		return
	}
	fields := logrus.Fields{"transaction": int64(tx.ID), "result": outcome.Result, "amount": int64(tx.TransferAmount)}
	if outcome.Order != nil {
		fields["order"] = outcome.Order.ID.Hex()
	}
	logrus.WithFields(fields).Info("SePay transaction handled")
	c.JSON(http.StatusOK, gin.H{"success": true, "result": outcome.Result})
}
