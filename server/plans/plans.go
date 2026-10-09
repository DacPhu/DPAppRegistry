// Package plans holds what each account's plan allows and checks requests against
// it. Enforcement is off unless PLANS_ENABLED is set, so a self-hosted registry stays
// unlimited; a hosted one sets each account's plan with the account command.
package plans

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/usage"
	"github.com/DacPhu/DPAppRegistry/server/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Feature is something a plan includes or leaves out.
type Feature string

const (
	PrivateApps Feature = "private_apps"
	// Rollouts covers staged rollouts and required intermediate versions.
	Rollouts      Feature = "rollouts"
	Statistics    Feature = "statistics"
	Reports       Feature = "reports"
	TUF           Feature = "tuf"
	StoreBranding Feature = "store_branding"
)

// featureNeeds starts the sentence that tells an account which plan a feature needs.
var featureNeeds = map[Feature]string{
	PrivateApps:   "Private apps need",
	Rollouts:      "Staged rollouts and required intermediate versions need",
	Statistics:    "Install statistics need",
	Reports:       "Crash reports need",
	TUF:           "TUF signing needs",
	StoreBranding: "Store branding needs",
}

// Limits caps what an account may hold. Zero means unlimited.
type Limits struct {
	Apps         int   `bson:"apps,omitempty" json:"apps"`
	StorageBytes int64 `bson:"storage_bytes,omitempty" json:"storage_bytes"`
	// Members counts the account itself and its team users.
	Members int `bson:"members,omitempty" json:"members"`
	// MAI is the monthly active installs the plan includes.
	MAI int64 `bson:"mai,omitempty" json:"mai"`
}

// Plan is one row of the price list (Mitekbro/docs/pricing-model.md).
type Plan struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Limits   Limits    `json:"limits"`
	Features []Feature `json:"features"`
	// HardMAICap stops uploads, never update checks, once the month's installs pass Limits.MAI.
	HardMAICap bool `json:"hard_mai_cap"`
	// The price of each 1,000 installs over Limits.MAI; zero when the plan has none.
	OverageUSD float64 `json:"overage_usd,omitempty"`
	OverageVND int64   `json:"overage_vnd,omitempty"`
}

const gib = int64(1) << 30

// Catalog lists the plans for sale, cheapest first.
var Catalog = []Plan{
	{ID: "hobby", Name: "Hobby", Limits: Limits{Apps: 2, StorageBytes: 5 * gib, Members: 1, MAI: 1000}, HardMAICap: true},
	{ID: "starter", Name: "Starter", Limits: Limits{Apps: 5, StorageBytes: 50 * gib, Members: 3, MAI: 10000},
		Features: []Feature{PrivateApps, StoreBranding}, OverageUSD: 2, OverageVND: 50000},
	{ID: "team", Name: "Team", Limits: Limits{Apps: 20, StorageBytes: 250 * gib, Members: 10, MAI: 50000},
		Features: []Feature{PrivateApps, Rollouts, Statistics, Reports, StoreBranding}, OverageUSD: 1.5, OverageVND: 40000},
	{ID: "business", Name: "Business", Limits: Limits{StorageBytes: 1024 * gib, MAI: 250000},
		Features: []Feature{PrivateApps, Rollouts, Statistics, Reports, TUF, StoreBranding}},
}

// Internal is the operator's own account: no limits, every feature. It is also what
// every account gets while plans are off.
var Internal = Plan{ID: "internal", Name: "Internal",
	Features: []Feature{PrivateApps, Rollouts, Statistics, Reports, TUF, StoreBranding}}

// Find returns the plan with this ID.
func Find(id string) (Plan, bool) {
	if id == Internal.ID {
		return Internal, true
	}
	for _, plan := range Catalog {
		if plan.ID == id {
			return plan, true
		}
	}
	return Plan{}, false
}

// Enabled reports whether plans are enforced on this server.
func Enabled() bool {
	return viper.GetBool("PLANS_ENABLED")
}

func defaultPlan() Plan {
	if plan, ok := Find(viper.GetString("PLAN_DEFAULT")); ok {
		return plan
	}
	return Catalog[0]
}

// Account is an account's plan with its own overrides applied.
type Account struct {
	Owner  string `json:"owner"`
	Plan   Plan   `json:"plan"`
	Limits Limits `json:"limits"`
}

// Has reports whether the account's plan includes feature.
func (a Account) Has(feature Feature) bool {
	for _, f := range a.Plan.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// Require returns a LimitError naming the cheapest plan with feature when the account lacks it.
func (a Account) Require(feature Feature) error {
	if a.Has(feature) {
		return nil
	}
	need := "a higher plan"
	for _, plan := range Catalog {
		if (Account{Plan: plan}).Has(feature) {
			need = fmt.Sprintf("the %s plan or higher", plan.Name)
			break
		}
	}
	return &LimitError{Plan: a.Plan.ID, Limit: string(feature),
		Message: fmt.Sprintf("%s %s; this account is on %s.", featureNeeds[feature], need, a.Plan.Name)}
}

// LimitError is a request the account's plan does not allow.
type LimitError struct {
	Plan    string
	Limit   string
	Message string
}

func (e *LimitError) Error() string { return e.Message }

// Respond answers the request when err is set, with 402 for a plan limit and 500
// otherwise, and reports whether it did.
func Respond(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var limit *LimitError
	if errors.As(err, &limit) {
		c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{"error": limit.Message, "code": "plan_limit", "limit": limit.Limit, "plan": limit.Plan})
		return true
	}
	logrus.Errorf("plans: %v", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to check the account's plan"})
	return true
}

var (
	database    *mongo.Database
	redisClient *redis.Client

	cacheMu sync.Mutex
	cache   = map[string]cachedAccount{}
)

type cachedAccount struct {
	account Account
	expires time.Time
}

const cacheFor = time.Minute

// Setup gives the package the databases it reads accounts and usage from.
func Setup(db *mongo.Database, rdb *redis.Client) {
	database = db
	redisClient = rdb
}

// accountDoc is the part of an admins document plans read.
type accountDoc struct {
	Plan       string `bson:"plan"`
	PlanLimits Limits `bson:"plan_limits"`
}

// Load returns owner's account. While plans are off every account is Internal.
func Load(ctx context.Context, owner string) (Account, error) {
	if !Enabled() {
		return Account{Owner: owner, Plan: Internal}, nil
	}
	cacheMu.Lock()
	cached, ok := cache[owner]
	cacheMu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.account, nil
	}

	account, err := LoadStored(ctx, owner)
	if err != nil {
		return Account{}, err
	}
	cacheMu.Lock()
	cache[owner] = cachedAccount{account: account, expires: time.Now().Add(cacheFor)}
	cacheMu.Unlock()
	return account, nil
}

// LoadStored reads owner's plan as stored, whether or not plans are enforced; the
// operator commands report from it.
func LoadStored(ctx context.Context, owner string) (Account, error) {
	var doc accountDoc
	err := database.Collection("admins").FindOne(ctx, bson.M{"username": owner}).Decode(&doc)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return Account{}, err
	}
	plan, found := Find(doc.Plan)
	if !found {
		plan = defaultPlan()
	}
	account := Account{Owner: owner, Plan: plan, Limits: plan.Limits}
	if o := doc.PlanLimits; o != (Limits{}) {
		if o.Apps != 0 {
			account.Limits.Apps = o.Apps
		}
		if o.StorageBytes != 0 {
			account.Limits.StorageBytes = o.StorageBytes
		}
		if o.Members != 0 {
			account.Limits.Members = o.Members
		}
		if o.MAI != 0 {
			account.Limits.MAI = o.MAI
		}
	}
	return account, nil
}

// Forget drops owner's cached account, after its plan changed.
func Forget(owner string) {
	cacheMu.Lock()
	delete(cache, owner)
	cacheMu.Unlock()
}

// ForRequest returns the account the request acts for: a team user's admin, or the caller.
func ForRequest(c *gin.Context) (Account, error) {
	username, err := utils.GetUsernameFromContext(c)
	if err != nil {
		return Account{}, err
	}
	owner, err := utils.ResolveRequestOwner(c.Request.Context(), username, database)
	if err != nil {
		return Account{}, err
	}
	return Load(c.Request.Context(), owner)
}

// HasFeature reports whether owner's plan includes feature; on a lookup error it answers
// yes, so a database hiccup never turns a paid feature off.
func HasFeature(ctx context.Context, owner string, feature Feature) bool {
	account, err := Load(ctx, owner)
	if err != nil {
		logrus.Warnf("plans: loading %s: %v", owner, err)
		return true
	}
	return account.Has(feature)
}

// CheckNewApp refuses another app once the account holds as many as its plan allows.
func (a Account) CheckNewApp(ctx context.Context) error {
	if a.Limits.Apps == 0 {
		return nil
	}
	count, err := database.Collection("apps_meta").CountDocuments(ctx, bson.M{"owner": a.Owner, "app_name": bson.M{"$exists": true}})
	if err != nil {
		return err
	}
	if count >= int64(a.Limits.Apps) {
		return &LimitError{Plan: a.Plan.ID, Limit: "apps",
			Message: fmt.Sprintf("The %s plan allows %d apps. Delete one or upgrade the plan.", a.Plan.Name, a.Limits.Apps)}
	}
	return nil
}

// CheckNewMember refuses another team user once the account and its team fill the plan's seats.
func (a Account) CheckNewMember(ctx context.Context) error {
	if a.Limits.Members == 0 {
		return nil
	}
	count, err := database.Collection("team_users").CountDocuments(ctx, bson.M{"owner": a.Owner})
	if err != nil {
		return err
	}
	if count+1 >= int64(a.Limits.Members) {
		return &LimitError{Plan: a.Plan.ID, Limit: "members",
			Message: fmt.Sprintf("The %s plan allows %d members, including the account itself. Remove one or upgrade the plan.", a.Plan.Name, a.Limits.Members)}
	}
	return nil
}

// CheckUpload refuses a new upload: on a plan with a hard cap, once this month's installs
// passed what the plan includes, and on any plan, incoming bytes it has no room for.
func (a Account) CheckUpload(ctx context.Context, incoming int64) error {
	if a.Plan.HardMAICap && a.Limits.MAI != 0 {
		mai, err := usage.MonthlyMAI(ctx, redisClient, a.Owner, usage.Month(time.Now()))
		if err != nil {
			return err
		}
		if mai.Total() > a.Limits.MAI {
			return &LimitError{Plan: a.Plan.ID, Limit: "mai",
				Message: fmt.Sprintf("This month your apps passed the %s plan's %d active installs. Updates keep working; upgrade the plan to upload new versions.", a.Plan.Name, a.Limits.MAI)}
		}
	}
	return a.CheckStorage(ctx, incoming)
}

// CheckStorage refuses incoming bytes the account has no room for.
func (a Account) CheckStorage(ctx context.Context, incoming int64) error {
	if a.Limits.StorageBytes == 0 || incoming <= 0 {
		return nil
	}
	used, err := usage.StorageBytes(ctx, database, a.Owner)
	if err != nil {
		return err
	}
	if used+incoming > a.Limits.StorageBytes {
		left := a.Limits.StorageBytes - used
		if left < 0 {
			left = 0
		}
		return &LimitError{Plan: a.Plan.ID, Limit: "storage",
			Message: fmt.Sprintf("This upload needs %s, but the %s plan has %s of its %s left. Delete old versions or upgrade the plan.",
				usage.FormatBytes(incoming), a.Plan.Name, usage.FormatBytes(left), usage.FormatBytes(a.Limits.StorageBytes))}
	}
	return nil
}

// CheckReleaseFlags refuses a staged rollout or a required intermediate version on a plan without them.
func (a Account) CheckReleaseFlags(params map[string]interface{}) error {
	staged := false
	if rollout, ok := params["rollout"]; ok {
		switch r := rollout.(type) {
		case int:
			staged = r < 100
		case float64:
			staged = r < 100
		case string:
			n, err := strconv.Atoi(r)
			staged = err == nil && n < 100
		}
	}
	if staged || utils.GetBoolParam(params["intermediate"]) {
		return a.Require(Rollouts)
	}
	return nil
}

// UploadGate refuses a new upload the account has no room for before its body is read,
// going by Content-Length. The handlers check the exact sizes again.
func UploadGate() gin.HandlerFunc {
	return gate(func(c *gin.Context, account Account) error {
		return account.CheckUpload(c.Request.Context(), c.Request.ContentLength)
	})
}

// StorageGate does the same for edits that may add files to a release. Unlike
// UploadGate it never stops them over the install cap: unpublishing a bad release
// must keep working.
func StorageGate() gin.HandlerFunc {
	return gate(func(c *gin.Context, account Account) error {
		return account.CheckStorage(c.Request.Context(), c.Request.ContentLength)
	})
}

// FeatureGate refuses every request on its routes from an account without feature.
func FeatureGate(feature Feature) gin.HandlerFunc {
	return gate(func(_ *gin.Context, account Account) error {
		return account.Require(feature)
	})
}

func gate(check func(*gin.Context, Account) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Enabled() {
			c.Next()
			return
		}
		account, err := ForRequest(c)
		if Respond(c, err) || Respond(c, check(c, account)) {
			return
		}
		c.Next()
	}
}
