package utils

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

// RateLimit allows Limit requests per Window, counted under Key.
type RateLimit struct {
	Key    string
	Limit  int64
	Window time.Duration
}

func (l RateLimit) bucketKey(now time.Time) string {
	return l.Key + ":" + strconv.FormatInt(now.Unix()/int64(l.Window/time.Second), 10)
}

// ClientIPKey names the caller for rate limiting. A single IPv6 host usually
// controls a whole /64, so per-address buckets would be trivially rotated.
func ClientIPKey(c *gin.Context) string {
	ip := net.ParseIP(c.ClientIP())
	if ip == nil {
		return c.ClientIP()
	}
	if ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return ip.String()
}

// EnforceRateLimits counts the request against each limit and answers 429 when
// one is exceeded, reporting whether the request may go on. It fails closed on
// a Redis error: these limits guard public endpoints that write to the database.
func EnforceRateLimits(c *gin.Context, rdb *redis.Client, limits []RateLimit, now time.Time) bool {
	if rdb == nil {
		return true
	}
	ctx := c.Request.Context()
	for _, l := range limits {
		key := l.bucketKey(now)
		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			logrus.Errorf("Rate limit check failed (rejecting request): %v", err)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "service temporarily unavailable"})
			return false
		}
		if count == 1 {
			rdb.Expire(ctx, key, l.Window)
		}
		if count > l.Limit {
			windowSeconds := int64(l.Window / time.Second)
			c.Header("Retry-After", strconv.FormatInt(windowSeconds-now.Unix()%windowSeconds, 10))
			logrus.Warnf("Rate limit exceeded for %s from %s", l.Key, c.ClientIP())
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests, try again later"})
			return false
		}
	}
	return true
}
