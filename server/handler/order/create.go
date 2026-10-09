package order

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/sepay"
	"github.com/DacPhu/DPAppRegistry/server/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	createPerIPPerHour   int64 = 5
	createGlobalPerHour  int64 = 50
	statusPerIPPerMinute int64 = 60
)

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

type createRequest struct {
	Product  string `json:"product" binding:"required"`
	Plan     string `json:"plan" binding:"required"`
	Quantity int    `json:"quantity"`
	Customer struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Company string `json:"company"`
	} `json:"customer"`
	Lang string `json:"lang"`
}

// storeOwner is the account whose products the store sells.
func storeOwner() string {
	if owner := viper.GetString("STORE_OWNER"); owner != "" {
		return owner
	}
	return "admin"
}

// Create answers POST /store/orders: starts a purchase and says how to pay.
func Create(c *gin.Context, database *mongo.Database, rdb *redis.Client) {
	if !utils.EnforceRateLimits(c, rdb, []utils.RateLimit{
		{Key: "order:rl:create:ip:" + utils.ClientIPKey(c), Limit: createPerIPPerHour, Window: time.Hour},
		{Key: "order:rl:create:global", Limit: createGlobalPerHour, Window: time.Hour},
	}, time.Now()) {
		return
	}
	if !Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the store is not taking orders right now"})
		return
	}
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if req.Quantity == 0 {
		req.Quantity = 1
	}
	name := strings.TrimSpace(req.Customer.Name)
	email := strings.ToLower(strings.TrimSpace(req.Customer.Email))
	company := strings.TrimSpace(req.Customer.Company)
	switch {
	case utf8.RuneCountInString(name) < 2 || utf8.RuneCountInString(name) > 120:
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 2 to 120 characters", "field": "name"})
		return
	case len(email) > 254 || !emailPattern.MatchString(email):
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid email is required", "field": "email"})
		return
	case utf8.RuneCountInString(company) > 120:
		c.JSON(http.StatusBadRequest, gin.H{"error": "company must be at most 120 characters", "field": "company"})
		return
	}
	lang := "vi"
	if req.Lang == "en" {
		lang = "en"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	order, err := license.CreateOrder(ctx, database, license.OrderRequest{
		Owner: storeOwner(), Product: req.Product, Plan: req.Plan, Quantity: req.Quantity,
		Customer: model.LicenseCustomer{Name: name, Email: email, Company: company}, Lang: lang,
	})
	if err != nil {
		if errors.Is(err, license.ErrNotForSale) || strings.HasPrefix(err.Error(), "quantity") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		logrus.Errorf("Failed to create order: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create the order"})
		return
	}
	c.Header("Cache-Control", "no-store")
	body := view(order, nil, time.Now())
	body["view_token"] = order.ViewToken
	c.JSON(http.StatusCreated, body)
}

// view renders an order for its buyer.
func view(order *model.Order, lic *model.License, now time.Time) gin.H {
	cfg := sepay.FromViper()
	body := gin.H{
		"id":           order.ID.Hex(),
		"status":       license.DisplayStatus(order, now),
		"product":      order.Product,
		"plan":         order.Plan,
		"quantity":     order.Quantity,
		"amount_vnd":   order.AmountVND,
		"payment_code": order.PaymentCode,
		"qr_url":       sepay.QRURL(cfg, order.AmountVND, order.PaymentCode),
		"bank":         gin.H{"name": cfg.BankName, "account": cfg.BankAccount, "holder": cfg.AccountHolder},
		"email":        order.Customer.Email,
		"created_at":   order.CreatedAt,
		"expires_at":   order.ExpiresAt,
		"paid_at":      order.PaidAt,
		"email_sent":   order.EmailSent,
		"url":          license.OrderURL(order),
		"license":      nil,
	}
	if lic != nil {
		body["license"] = gin.H{"id": lic.ID.Hex(), "key": lic.Key, "plan": lic.Plan, "clusters": lic.Clusters, "expires_at": lic.ExpiresAt, "updates_until": lic.UpdatesUntil}
	}
	return body
}
