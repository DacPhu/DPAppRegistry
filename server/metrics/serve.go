package metrics

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// The loopback, when METRICS_ALLOW names no one.
const defaultAllow = "127.0.0.0/8,::1/128"

// Serve answers GET /metrics on addr (METRICS_ADDR, e.g. ":9326"), on a
// listener of its own beside the API, in the background. Only clients inside
// allow (METRICS_ALLOW: comma-separated CIDRs, the loopback when empty) get
// an answer; everyone else gets 403, whatever headers they send. An empty
// addr serves nothing.
func Serve(addr, allow string) error {
	if addr == "" {
		return nil
	}
	prefixes, err := ParseAllow(allow)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           handler(prefixes),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
	}
	go func() {
		if err := server.Serve(listener); err != nil {
			logrus.Errorf("The metrics listener on %s stopped: %v", addr, err)
		}
	}()
	logrus.Infof("Serving /metrics on %s to %v", listener.Addr(), prefixes)
	return nil
}

// ParseAllow reads METRICS_ALLOW: comma-separated CIDRs, or the loopback when
// empty.
func ParseAllow(allow string) ([]netip.Prefix, error) {
	if strings.TrimSpace(allow) == "" {
		allow = defaultAllow
	}
	var prefixes []netip.Prefix
	for _, cidr := range strings.Split(allow, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil {
			return nil, fmt.Errorf("METRICS_ALLOW: %w", err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func handler(allow []netip.Prefix) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	return onlyFrom(allow, mux)
}

// onlyFrom passes on the requests of clients inside allow. The address is the
// connection's own: Prometheus reaches the listener directly, never through a
// proxy, so a forwarding header would only be a client's claim.
func onlyFrom(allow []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if client, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
			ip := client.Addr().Unmap()
			for _, prefix := range allow {
				if prefix.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
}
