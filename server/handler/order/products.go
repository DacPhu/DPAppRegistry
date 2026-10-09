// Package order is the store's checkout: the catalog, orders paid by bank
// transfer through SePay, and the webhook that fulfils them.
package order

import (
	"net/http"

	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/sepay"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// Enabled reports whether orders can be taken: switched on, able to issue
// keys, and with a bank account to show.
func Enabled() bool {
	return viper.GetBool("ORDERS_ENABLED") && license.Enabled() && sepay.FromViper().Ready()
}

// Products answers GET /store/products: what is for sale and at what price.
func Products(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, gin.H{"products": license.Products, "currency": "VND", "orders_enabled": Enabled(), "contact": viper.GetString("PLANS_CONTACT")})
}
