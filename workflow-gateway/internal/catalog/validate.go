package catalog

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateExternalURL checks that the URL is external and not cluster-internal.
func ValidateExternalURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if parsed.Scheme == "" {
		return fmt.Errorf("no URL scheme (http or https required)")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s (only http/https allowed)", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}
	hostLower := strings.ToLower(host)
	hostLower = strings.TrimSuffix(hostLower, ".")
	if strings.HasSuffix(hostLower, ".svc") || strings.HasSuffix(hostLower, ".svc.cluster.local") {
		return fmt.Errorf("cluster-internal Service DNS not allowed: %s", host)
	}
	if !strings.Contains(hostLower, ".") {
		return fmt.Errorf("non-external host (single-label, likely in-cluster): %s", host)
	}
	return nil
}
