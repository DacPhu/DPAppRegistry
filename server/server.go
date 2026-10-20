package server

import (
	db "github.com/DacPhu/DPAppRegistry/mongod"
	"github.com/DacPhu/DPAppRegistry/redisdb"
	"github.com/DacPhu/DPAppRegistry/server/handler"
	"github.com/DacPhu/DPAppRegistry/server/handler/order"
	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/metrics"
	"github.com/DacPhu/DPAppRegistry/server/plans"
	"github.com/DacPhu/DPAppRegistry/server/sepay"
	"github.com/DacPhu/DPAppRegistry/server/tuf"
	"github.com/DacPhu/DPAppRegistry/server/utils"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func StartServer(config *viper.Viper) {
	mongoUrl := config.GetString("MONGODB_URL")

	router := gin.New()
	if trustedProxies := config.GetString("TRUSTED_PROXIES"); trustedProxies != "" {
		proxies := strings.Split(trustedProxies, ",")
		for i := range proxies {
			proxies[i] = strings.TrimSpace(proxies[i])
		}
		if err := router.SetTrustedProxies(proxies); err != nil {
			logrus.Fatalf("Invalid TRUSTED_PROXIES: %v", err)
		}
	}
	// The metrics come before Recovery, so they see the 500 it answers a panic with.
	router.Use(gin.Logger(), metrics.Middleware(), gin.Recovery())

	client, configDB := db.ConnectToDatabase(mongoUrl)

	db := db.NewAppRepository(&configDB, client)

	mongoDatabase := client.Database(configDB.Database)

	logrus.Infoln("Redis connection is required. Connecting to Redis.")
	redisConfig := redisdb.RedisConfig{
		Addr:     config.GetString("REDIS_HOST") + ":" + config.GetString("REDIS_PORT"),
		Password: config.GetString("REDIS_PASSWORD"),
		DB:       config.GetInt("REDIS_DB"),
	}
	redisClient := redisdb.ConnectToRedis(redisConfig)

	handler := handler.NewAppHandler(client, db, mongoDatabase, redisClient, config.GetBool("PERFORMANCE_MODE"))
	plans.Setup(mongoDatabase, redisClient)
	if err := license.Setup(config.GetString("LICENSE_SIGNING_KEY")); err != nil {
		logrus.Fatalf("Invalid LICENSE_SIGNING_KEY: %v", err)
	}
	if plans.Enabled() {
		logrus.Infof("Plans are enforced; accounts without one get %q", config.GetString("PLAN_DEFAULT"))
	}
	os.Setenv("API_KEY", config.GetString("API_KEY"))
	// Add authentication middleware to required paths
	authMiddleware := utils.AuthMiddleware(mongoDatabase)

	router.GET("/health", handler.HealthCheck)

	allowedCORS := config.GetString("ALLOWED_CORS")
	allowedOrigins := strings.Split(allowedCORS, ",")

	router.Use(corsMiddleware(allowedOrigins))

	// Add squirrel_windows updater compatibility
	router.GET("/update/:owner/:app/:channel/:platform/:arch/:version/RELEASES", handler.SquirrelReleases)

	// Velopack dynamic feed
	router.GET("/velopack/:owner/:app/:platform/:arch/*feed", handler.FindVelopackFeed)

	router.GET("/checkVersion", handler.FindLatestVersion)
	router.GET("/apps/latest", handler.FetchLatestVersionOfApp)
	router.GET("/store/apps", handler.StoreApps)
	// An app reports the cluster it runs under a license on; the key is valid offline either way.
	router.POST("/store/licenses/activate", handler.ActivateLicense)
	// The store's checkout: the catalog is always there, orders only when the
	// registry can issue keys and has a bank account to be paid into.
	router.GET("/store/products", handler.StoreProducts)
	if order.Enabled() {
		router.POST("/store/orders", handler.CreateOrder)
		router.GET("/store/orders/:id", handler.OrderStatus)
		if sepay.FromViper().WebhookConfigured() {
			router.POST("/webhooks/sepay", handler.SePayWebhook)
		} else {
			logrus.Warn("Orders are on but no SEPAY_WEBHOOK_SECRET or SEPAY_WEBHOOK_APIKEY is set: transfers are matched only by `order reconcile`")
		}
		logrus.Info("The store is taking orders")
	} else if config.GetBool("ORDERS_ENABLED") {
		logrus.Warn("ORDERS_ENABLED is set but orders are off: LICENSE_SIGNING_KEY, SEPAY_BANK_ACCOUNT and SEPAY_BANK_NAME are all needed")
	}
	router.GET("/telemetry/beacon", telemetryMiddleware(config), handler.TelemetryBeacon)
	if config.GetString("API_KEY") != "" {
		router.POST("/signup", handler.SignUp)
	} else {
		logrus.Warnln("API_KEY is not set, /signup is disabled")
	}
	router.POST("/login", handler.Login)

	if config.GetBool("REPORTS_ENABLED") {
		router.POST("/reports/ingest", handler.IngestReport)
	}

	// Access to private artifacts is decided per app inside the handler, so the route stays outside authMiddleware.
	router.GET("/download", handler.DownloadArtifact)
	router.Use(authMiddleware)

	// App routes
	// router.GET("/", handler.GetAllApps)
	router.GET("/whoami", handler.Whoami)
	router.POST("/upload", utils.CheckPermission(utils.PermissionUpload, utils.ResourceApps, mongoDatabase), plans.UploadGate(), handler.UploadApp)
	router.POST("/upload/init", utils.CheckPermission(utils.PermissionUpload, utils.ResourceApps, mongoDatabase), handler.InitPresignedUpload)
	router.POST("/upload/complete", utils.CheckPermission(utils.PermissionUpload, utils.ResourceApps, mongoDatabase), handler.CompletePresignedUpload)
	router.POST("/apps/update", utils.CheckPermission(utils.PermissionEdit, utils.ResourceApps, mongoDatabase), plans.StorageGate(), handler.UpdateSpecificApp)
	router.POST("/app/update", utils.CheckPermission(utils.PermissionEdit, utils.ResourceApps, mongoDatabase), handler.UpdateApp)
	router.DELETE("/apps/delete", utils.CheckPermission(utils.PermissionDelete, utils.ResourceApps, mongoDatabase), handler.DeleteSpecificVersionOfApp)
	router.GET("/search", handler.GetAppByName)

	// Channel routes
	router.POST("/channel/create", utils.CheckPermission(utils.PermissionCreate, utils.ResourceChannels, mongoDatabase), handler.CreateChannel)
	router.GET("/channel/list", handler.ListChannels)
	router.DELETE("/channel/delete", utils.CheckPermission(utils.PermissionDelete, utils.ResourceChannels, mongoDatabase), handler.DeleteChannel)
	router.POST("/channel/update", utils.CheckPermission(utils.PermissionEdit, utils.ResourceChannels, mongoDatabase), handler.UpdateChannel)

	// Platform routes
	router.POST("/platform/create", utils.CheckPermission(utils.PermissionCreate, utils.ResourcePlatforms, mongoDatabase), handler.CreatePlatform)
	router.GET("/platform/list", handler.ListPlatforms)
	router.DELETE("/platform/delete", utils.CheckPermission(utils.PermissionDelete, utils.ResourcePlatforms, mongoDatabase), handler.DeletePlatform)
	router.POST("/platform/update", utils.CheckPermission(utils.PermissionEdit, utils.ResourcePlatforms, mongoDatabase), handler.UpdatePlatform)

	// Arch routes
	router.POST("/arch/create", utils.CheckPermission(utils.PermissionCreate, utils.ResourceArchs, mongoDatabase), handler.CreateArch)
	router.GET("/arch/list", handler.ListArchs)
	router.DELETE("/arch/delete", utils.CheckPermission(utils.PermissionDelete, utils.ResourceArchs, mongoDatabase), handler.DeleteArch)
	router.POST("/arch/update", utils.CheckPermission(utils.PermissionEdit, utils.ResourceArchs, mongoDatabase), handler.UpdateArch)

	// App management routes
	router.POST("/app/create", utils.CheckPermission(utils.PermissionCreate, utils.ResourceApps, mongoDatabase), handler.CreateApp)
	router.GET("/app/list", handler.ListApps)
	router.DELETE("/app/delete", utils.CheckPermission(utils.PermissionDelete, utils.ResourceApps, mongoDatabase), handler.DeleteApp)
	router.POST("/artifact/delete", utils.CheckPermission(utils.PermissionDelete, utils.ResourceApps, mongoDatabase), handler.DeleteSpecificArtifactOfApp)

	// Team user management - only admins can create team users
	router.POST("/user/create", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.CreateTeamUser)
	router.POST("/user/update", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.UpdateTeamUser)
	router.GET("/users/list", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.ListTeamUsers)
	router.DELETE("/user/delete", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.DeleteTeamUser)
	router.POST("/admin/update", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.UpdateAdmin)

	// Telemetry endpoint
	router.GET("/telemetry", authMiddleware, telemetryMiddleware(config), plans.FeatureGate(plans.Statistics), handler.GetTelemetry)

	// The account's plan and what it uses
	router.GET("/account/plan", handler.AccountPlan)

	// Licenses the admin sells for their products
	router.POST("/license/create", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.CreateLicense)
	router.GET("/license/list", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.ListLicenses)
	router.GET("/license/show", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.ShowLicense)
	router.POST("/license/revoke", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.RevokeLicense)
	router.GET("/order/list", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.ListOrders)

	// Token routes
	router.POST("/token/create", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.CreateToken)
	router.GET("/token/list", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.ListTokens)
	router.DELETE("/token/delete", authMiddleware, utils.AdminOnlyMiddleware(mongoDatabase), handler.DeleteToken)

	// Reports routes
	router.GET("/report-keys/list", utils.CheckPermission(utils.PermissionEdit, utils.ResourceApps, mongoDatabase), handler.ListReportKeys)
	router.POST("/report-keys/regenerate", utils.CheckPermission(utils.PermissionEdit, utils.ResourceApps, mongoDatabase), handler.RegenerateReportKey)

	// Download token routes
	router.GET("/download-tokens/list", utils.CheckPermission(utils.PermissionEdit, utils.ResourceApps, mongoDatabase), handler.ListDownloadTokens)
	router.POST("/download-tokens/regenerate", utils.CheckPermission(utils.PermissionEdit, utils.ResourceApps, mongoDatabase), handler.RegenerateDownloadToken)

	// Reports read API (admin + team users scoped to their allowed apps)
	if config.GetBool("REPORTS_ENABLED") {
		reportsGate := plans.FeatureGate(plans.Reports)
		router.GET("/reports/groups", utils.CheckPermission(utils.PermissionDownload, utils.ResourceApps, mongoDatabase), reportsGate, handler.ListReportGroups)
		router.GET("/reports/groups/:groupHash/blobs", utils.CheckPermission(utils.PermissionDownload, utils.ResourceApps, mongoDatabase), reportsGate, handler.ListReportGroupBlobs)
		router.PATCH("/reports/groups/:groupHash", utils.CheckPermission(utils.PermissionEdit, utils.ResourceApps, mongoDatabase), reportsGate, handler.UpdateReportGroup)
		router.DELETE("/reports/groups/:groupHash", utils.CheckPermission(utils.PermissionDelete, utils.ResourceApps, mongoDatabase), reportsGate, handler.DeleteReportGroup)
	}

	// TUF routes
	if config.GetBool("TUF_ENABLED") {
		// Middleware added here reaches only the routes registered after it, the TUF ones,
		// and runs after the authentication added above.
		router.Use(plans.FeatureGate(plans.TUF))
		tuf.SetupRoutes(router, authMiddleware, mongoDatabase, redisClient, db)
	}

	metrics.TrackRoutes(router.Routes())
	if err := metrics.Serve(config.GetString("METRICS_ADDR"), config.GetString("METRICS_ALLOW")); err != nil {
		logrus.Errorf("Not serving /metrics: %v", err)
	}

	// get the port from the configuration file
	port := config.GetString("PORT")
	if port == "" {
		port = "9000"
	}
	router.Run(":" + port)
}

func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if allowedOrigin == origin {
				allowed = true
				break
			}
		}

		if allowed {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Download-Token, accept, origin, Cache-Control, X-Requested-With")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func telemetryMiddleware(config *viper.Viper) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.GetBool("ENABLE_TELEMETRY") {
			c.JSON(403, gin.H{
				"error": "Telemetry is not enabled on this instance",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
