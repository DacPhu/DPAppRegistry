// Package usage measures what an account uses: its monthly active installs, counted
// in Redis HyperLogLogs as update checks arrive, and the storage its releases take.
package usage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Counters outlive their month long enough to invoice from and to compare a year back.
const keyTTL = 400 * 24 * time.Hour

// Month names a calendar month, in UTC, the way the counters do: "2026-10".
func Month(t time.Time) string {
	return t.UTC().Format("2006-01")
}

// Installs reported by the SDKs (X-Device-ID) and installs estimated for checks that
// came without one are counted apart, so an invoice can tell them apart.
func measuredKey(owner, month, app string) string {
	return fmt.Sprintf("mai:%s:%s:%s", owner, month, app)
}
func estimatedKey(owner, month, app string) string {
	return fmt.Sprintf("mai_est:%s:%s:%s", owner, month, app)
}
func appsKey(owner, month string) string { return fmt.Sprintf("mai_apps:%s:%s", owner, month) }

// EstimatedID stands in for an install that sent no X-Device-ID: a hash of who asked
// and for which build. Behind one NAT several installs count as one, so it errs low.
func EstimatedID(c *gin.Context, platform, arch string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{c.ClientIP(), c.GetHeader("User-Agent"), platform, arch}, "|")))
	return hex.EncodeToString(sum[:16])
}

// RecordInstall counts one update check toward owner's active installs this month.
// installID is the X-Device-ID the client sent; without one, estimatedID is counted
// as an estimate instead. Failures are logged and never fail the check.
func RecordInstall(ctx context.Context, rdb *redis.Client, owner, app, installID, estimatedID string, now time.Time) {
	if rdb == nil || owner == "" || app == "" {
		return
	}
	month := Month(now)
	key, member := measuredKey(owner, month, app), installID
	if installID == "" {
		key, member = estimatedKey(owner, month, app), estimatedID
	}
	if member == "" {
		return
	}
	pipe := rdb.Pipeline()
	pipe.SAdd(ctx, appsKey(owner, month), app)
	pipe.Expire(ctx, appsKey(owner, month), keyTTL)
	pipe.PFAdd(ctx, key, member)
	pipe.Expire(ctx, key, keyTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		logrus.Debugf("usage: counting an install of %s/%s: %v", owner, app, err)
	}
}

// AppInstalls is one app's active installs in a month.
type AppInstalls struct {
	App       string `bson:"app" json:"app"`
	Measured  int64  `bson:"measured" json:"measured"`
	Estimated int64  `bson:"estimated" json:"estimated"`
}

// Installs is an account's active installs in a month, summed over its apps.
type Installs struct {
	Month     string        `bson:"month" json:"month"`
	Measured  int64         `bson:"measured" json:"measured"`
	Estimated int64         `bson:"estimated" json:"estimated"`
	Apps      []AppInstalls `bson:"apps" json:"apps"`
}

// Total is what a plan's MAI limit is compared with.
func (i Installs) Total() int64 { return i.Measured + i.Estimated }

// MonthlyMAI counts owner's active installs in month: per app, distinct installs, then summed.
func MonthlyMAI(ctx context.Context, rdb *redis.Client, owner, month string) (Installs, error) {
	result := Installs{Month: month, Apps: []AppInstalls{}}
	if rdb == nil {
		return result, nil
	}
	apps, err := rdb.SMembers(ctx, appsKey(owner, month)).Result()
	if err != nil {
		return result, err
	}
	sort.Strings(apps)
	for _, app := range apps {
		measured, err := rdb.PFCount(ctx, measuredKey(owner, month, app)).Result()
		if err != nil {
			return result, err
		}
		estimated, err := rdb.PFCount(ctx, estimatedKey(owner, month, app)).Result()
		if err != nil {
			return result, err
		}
		result.Apps = append(result.Apps, AppInstalls{App: app, Measured: measured, Estimated: estimated})
		result.Measured += measured
		result.Estimated += estimated
	}
	return result, nil
}

// StorageBytes is the size of every file in owner's releases, as recorded at upload.
func StorageBytes(ctx context.Context, db *mongo.Database, owner string) (int64, error) {
	cursor, err := db.Collection("apps").Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"owner": owner}}},
		{{Key: "$unwind", Value: "$artifacts"}},
		{{Key: "$group", Value: bson.M{"_id": nil, "total": bson.M{"$sum": "$artifacts.length"}}}},
	})
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		Total int64 `bson:"total"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Total, nil
}

// Snapshot is one account's usage in one month, kept in the usage collection.
type Snapshot struct {
	Owner        string    `bson:"owner" json:"owner"`
	Month        string    `bson:"month" json:"month"`
	Installs     Installs  `bson:"installs" json:"installs"`
	StorageBytes int64     `bson:"storage_bytes" json:"storage_bytes"`
	UpdatedAt    time.Time `bson:"updated_at" json:"updated_at"`
}

// TakeSnapshots writes this month's installs and storage of every account to the usage
// collection, so database backups keep the history invoices are made from.
func TakeSnapshots(ctx context.Context, db *mongo.Database, rdb *redis.Client, now time.Time) ([]Snapshot, error) {
	owners, err := db.Collection("admins").Distinct(ctx, "username", bson.M{})
	if err != nil {
		return nil, err
	}
	month := Month(now)
	snapshots := make([]Snapshot, 0, len(owners))
	for _, value := range owners {
		owner, ok := value.(string)
		if !ok || owner == "" {
			continue
		}
		installs, err := MonthlyMAI(ctx, rdb, owner, month)
		if err != nil {
			return snapshots, err
		}
		storage, err := StorageBytes(ctx, db, owner)
		if err != nil {
			return snapshots, err
		}
		snapshot := Snapshot{Owner: owner, Month: month, Installs: installs, StorageBytes: storage, UpdatedAt: now.UTC()}
		_, err = db.Collection("usage").UpdateOne(ctx, bson.M{"owner": owner, "month": month},
			bson.M{"$set": snapshot}, options.Update().SetUpsert(true))
		if err != nil {
			return snapshots, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// FormatBytes writes a size the way people read it: 812 KB, 4.2 GB.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, exp := float64(n)/unit, 0
	for value >= unit && exp < 4 {
		value /= unit
		exp++
	}
	if value < 10 {
		return fmt.Sprintf("%.1f %cB", value, "KMGTP"[exp])
	}
	return fmt.Sprintf("%.0f %cB", value, "KMGTP"[exp])
}
