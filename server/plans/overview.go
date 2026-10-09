package plans

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/usage"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
)

// Usage is what an account holds and how many installs checked in this month.
type Usage struct {
	Apps         int64          `json:"apps"`
	Members      int64          `json:"members"`
	StorageBytes int64          `json:"storage_bytes"`
	Installs     usage.Installs `json:"installs"`
}

// Overview is an account's plan next to its usage, for the dashboard and the operator.
type Overview struct {
	Account
	Enabled  bool   `json:"enabled"`
	Usage    Usage  `json:"usage"`
	StoreURL string `json:"store_url,omitempty"`
	// Contact is whom to ask for another plan (PLANS_CONTACT).
	Contact string `json:"contact,omitempty"`
}

// OverviewOf measures what account uses.
func OverviewOf(ctx context.Context, account Account) (Overview, error) {
	overview := Overview{Account: account, Enabled: Enabled(), StoreURL: StoreURL(account.Owner), Contact: viper.GetString("PLANS_CONTACT")}
	var err error
	if overview.Usage.Apps, err = database.Collection("apps_meta").CountDocuments(ctx, bson.M{"owner": account.Owner, "app_name": bson.M{"$exists": true}}); err != nil {
		return overview, err
	}
	teamUsers, err := database.Collection("team_users").CountDocuments(ctx, bson.M{"owner": account.Owner})
	if err != nil {
		return overview, err
	}
	overview.Usage.Members = teamUsers + 1
	if overview.Usage.StorageBytes, err = usage.StorageBytes(ctx, database, account.Owner); err != nil {
		return overview, err
	}
	overview.Usage.Installs, err = usage.MonthlyMAI(ctx, redisClient, account.Owner, usage.Month(time.Now()))
	return overview, err
}

// StoreURL is the account's page in the public store: the store's root for STORE_OWNER,
// /@<account>/ for everyone else. Empty when STORE_URL is not set.
func StoreURL(owner string) string {
	base := strings.TrimRight(viper.GetString("STORE_URL"), "/")
	if base == "" {
		return ""
	}
	storeOwner := viper.GetString("STORE_OWNER")
	if storeOwner == "" {
		storeOwner = "admin"
	}
	if owner == storeOwner {
		return base + "/"
	}
	return fmt.Sprintf("%s/@%s/", base, url.PathEscape(owner))
}

// Handle answers GET /account/plan for the account the caller acts for.
func Handle(c *gin.Context) {
	account, err := ForRequest(c)
	if Respond(c, err) {
		return
	}
	overview, err := OverviewOf(c.Request.Context(), account)
	if Respond(c, err) {
		return
	}
	c.JSON(http.StatusOK, overview)
}
