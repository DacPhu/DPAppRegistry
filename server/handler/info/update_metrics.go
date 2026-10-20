package info

import (
	"github.com/DacPhu/DPAppRegistry/server/metrics"

	"github.com/gin-gonic/gin"
)

// countUpdateCheck counts one answer to an update check. Its app, channel,
// platform and arch become labels only as a set the telemetry allow-list
// knows, one the registry has published a release for; any other set counts
// as "other", so clients can't create series. The version and the device are
// never labels.
func countUpdateCheck(params map[string]interface{}, result string) {
	owner, _ := params["owner"].(string)
	appName, _ := params["app_name"].(string)
	channel, _ := params["channel"].(string)
	platform, _ := params["platform"].(string)
	arch, _ := params["arch"].(string)
	if !LoadTelemetryAllowList().Valid(owner, appName, channel, platform, arch) {
		metrics.UpdateCheck(metrics.Other, metrics.Other, metrics.Other, metrics.Other, result)
		return
	}
	metrics.UpdateCheck(owner+"/"+appName, channel, platform, arch, result)
}

// checkQuery is what an update check asked for, to count one whose
// parameters didn't validate.
func checkQuery(c *gin.Context) map[string]interface{} {
	return map[string]interface{}{
		"owner":    c.Query("owner"),
		"app_name": c.Query("app_name"),
		"channel":  c.Query("channel"),
		"platform": c.Query("platform"),
		"arch":     c.Query("arch"),
	}
}

// trackUpdateChecks starts the series of every set in the allow-list at 0.
func trackUpdateChecks(list *TelemetryAllowList) {
	if list == nil {
		return
	}
	for owner, ownerEntry := range list.Owners {
		for appName, appEntry := range ownerEntry.Apps {
			for channel, channelEntry := range appEntry.Channels {
				for platform, platformEntry := range channelEntry.Platforms {
					for arch := range platformEntry.Archs {
						metrics.TrackUpdateChecks(owner+"/"+appName, channel, platform, arch)
					}
				}
			}
		}
	}
}
