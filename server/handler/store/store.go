// Package store serves the catalog behind the public store page: every app
// that isn't private, with the installers of its latest published release.
package store

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	db "github.com/DacPhu/DPAppRegistry/mongod"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/plans"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const defaultChannel = "stable"

// Update metadata that can sit beside the installers without being marked as
// a feed (electron-builder's blockmaps and latest*.yml, Tauri's signatures).
var metadataPackages = map[string]bool{"blockmap": true, "yml": true, "yaml": true, "sig": true}

type download struct {
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	Package  string `json:"package"`
	URL      string `json:"url"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

// profile is what an account shows on its own store page instead of the store's
// defaults: set with the account store command, shown on plans with store branding.
type profile struct {
	Name    string            `bson:"name" json:"name,omitempty"`
	Tagline map[string]string `bson:"tagline" json:"tagline,omitempty"`
	About   map[string]string `bson:"about" json:"about,omitempty"`
	Logo    string            `bson:"logo" json:"logo,omitempty"`
	HomeURL string            `bson:"home_url" json:"homeUrl,omitempty"`
	Contact string            `bson:"contact" json:"contact,omitempty"`
	Links   []struct {
		Label map[string]string `bson:"label" json:"label"`
		URL   string            `bson:"url" json:"url"`
	} `bson:"links" json:"links,omitempty"`
}

type app struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Logo        string     `json:"logo,omitempty"`
	Version     string     `json:"version"`
	Channel     string     `json:"channel"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Changes     string     `json:"changes,omitempty"`
	Downloads   []download `json:"downloads"`
}

// ListApps answers GET /store/apps. The owner whose apps are listed comes from
// ?owner, then STORE_OWNER, then "admin"; the channel from ?channel, then
// "stable". Apps with nothing published on that channel are left out.
//
// With ?owner, the page is that account's own store: an account that doesn't
// exist is a 404, and the answer names the account and carries its profile.
func ListApps(c *gin.Context, repository db.AppRepository, database *mongo.Database) {
	requested := c.Query("owner")
	owner := requested
	if owner == "" {
		owner = viper.GetString("STORE_OWNER")
	}
	if owner == "" {
		owner = "admin"
	}
	channel := c.DefaultQuery("channel", defaultChannel)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	body := gin.H{}
	if requested != "" && database != nil {
		var account struct {
			StoreProfile *profile `bson:"store_profile"`
		}
		err := database.Collection("admins").FindOne(ctx, bson.M{"username": owner}).Decode(&account)
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "there is no store for this account"})
			return
		}
		if err != nil {
			logrus.Errorf("store: reading the account %s: %v", owner, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list apps"})
			return
		}
		body["account"] = owner
		if account.StoreProfile != nil && plans.HasFeature(ctx, owner, plans.StoreBranding) {
			body["profile"] = account.StoreProfile
		}
	}

	apps, err := repository.ListApps(ctx, owner)
	if err != nil {
		logrus.Errorf("store: listing apps of %s: %v", owner, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list apps"})
		return
	}

	listed := []app{}
	for _, meta := range apps {
		if meta.Private {
			continue
		}
		versions, err := repository.FetchLatestVersionOfApp(meta.AppName, channel, ctx, owner)
		if err != nil || len(versions) == 0 {
			// No such channel, or nothing published on it yet.
			logrus.Debugf("store: skipping %s: %v", meta.AppName, err)
			continue
		}
		entry := fromRelease(meta, versions[0], channel)
		if len(entry.Downloads) > 0 {
			listed = append(listed, entry)
		}
	}
	sort.Slice(listed, func(i, j int) bool {
		return strings.ToLower(listed[i].Name) < strings.ToLower(listed[j].Name)
	})

	body["apps"] = listed
	c.Header("Cache-Control", "public, max-age=60")
	c.JSON(http.StatusOK, body)
}

func fromRelease(meta *model.App, release *model.SpecificAppWithoutIDs, channel string) app {
	entry := app{
		Name:        meta.AppName,
		Description: meta.Description,
		Logo:        meta.Logo,
		Version:     release.Version,
		Channel:     channel,
		UpdatedAt:   release.UpdatedAt.Time().UTC(),
		Downloads:   []download{},
	}
	for _, change := range release.Changelog {
		if change.Version == release.Version {
			entry.Changes = change.Changes
			break
		}
	}
	for _, artifact := range release.Artifacts {
		// Feed files (RELEASES, latest.yml, appcast…) are for updaters, not people.
		pkg := strings.ToLower(strings.TrimPrefix(artifact.Package, "."))
		if artifact.IsFeed || artifact.Link == "" || metadataPackages[pkg] {
			continue
		}
		entry.Downloads = append(entry.Downloads, download{
			Platform: artifact.Platform,
			Arch:     artifact.Arch,
			Package:  pkg,
			URL:      artifact.Link,
			Size:     artifact.Length,
			SHA256:   artifact.Hashes["sha256"],
		})
	}
	return entry
}
