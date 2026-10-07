package discovery

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateExternalURL checks that the URL is external and not cluster-internal.
// Returns nil if valid, an error if the URL is cluster-internal or malformed.
func ValidateExternalURL(raw string) error {
	// Parse the URL
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}

	// If no scheme was present, Parse treats the whole thing as a path.
	// We require a scheme.
	if parsed.Scheme == "" {
		return fmt.Errorf("no URL scheme (http or https required)")
	}

	// Only http and https are allowed.
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s (only http/https allowed)", parsed.Scheme)
	}

	// Extract the host (without port).
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}

	// Normalize to lowercase and strip trailing root dot. A valid FQDN may end with
	// a root dot (e.g., "hello.default.svc.cluster.local."). Strip it before
	// validation so the suffix checks don't miss cluster-internal hostnames.
	hostLower := strings.ToLower(host)
	hostLower = strings.TrimSuffix(hostLower, ".")

	// Reject cluster-internal Service DNS (name.namespace.svc[.cluster.local]).
	// The ".svc" suffix already covers the bare name.namespace.svc form.
	if strings.HasSuffix(hostLower, ".svc") || strings.HasSuffix(hostLower, ".svc.cluster.local") {
		return fmt.Errorf("cluster-internal Service DNS not allowed: %s", host)
	}

	// Reject single-label hosts (e.g. "myservice"): a public URL needs a dotted
	// FQDN, so a bare label is an in-cluster same-namespace short form.
	if !strings.Contains(hostLower, ".") {
		return fmt.Errorf("non-external host (single-label, likely in-cluster): %s", host)
	}

	return nil
}
