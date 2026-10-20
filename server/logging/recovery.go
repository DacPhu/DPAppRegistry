package logging

import (
	"net/http"
	"runtime/debug"

	"github.com/DacPhu/DPAppRegistry/server/metrics"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Recovery answers a handler's panic with a 500 and logs it as an ERROR: the
// method, the route template, the panic and the stack, never the query string
// or the headers, which carry tokens. (gin's own Recovery writes to stderr,
// past this log, and dumps the request line and headers when the client has
// hung up.) A client that hung up gets no answer and no line.
func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err any) {
		logrus.Errorf("Panic answering %s %s: %v\n%s", metrics.Method(c.Request.Method), metrics.Route(c), err, debug.Stack())
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}
