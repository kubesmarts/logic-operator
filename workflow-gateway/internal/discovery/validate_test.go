package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateExternalURLAcceptsHTTP(t *testing.T) {
	err := ValidateExternalURL("http://hello.example.com")
	assert.NoError(t, err)
}

func TestValidateExternalURLAcceptsHTTPS(t *testing.T) {
	err := ValidateExternalURL("https://hello.example.com")
	assert.NoError(t, err)
}

func TestValidateExternalURLAcceptsPortNumber(t *testing.T) {
	err := ValidateExternalURL("http://hello.example.com:8080")
	assert.NoError(t, err)
}

func TestValidateExternalURLRejectsServiceDNS(t *testing.T) {
	tests := []string{
		"hello.default.svc.cluster.local",
		"http://hello.default.svc.cluster.local",
		"https://hello.default.svc",
		"http://hello.default.svc",
		"hello.default.svc",
	}
	for _, raw := range tests {
		err := ValidateExternalURL(raw)
		assert.Error(t, err, "should reject %s", raw)
	}
}

func TestValidateExternalURLRejectsEmptyHost(t *testing.T) {
	err := ValidateExternalURL("")
	assert.Error(t, err)
}

func TestValidateExternalURLRejectsNonHTTPScheme(t *testing.T) {
	err := ValidateExternalURL("tcp://hello.example.com")
	assert.Error(t, err)
}

func TestValidateExternalURLRejectsSingleLabelHost(t *testing.T) {
	for _, raw := range []string{"http://myservice", "https://myservice:8080"} {
		err := ValidateExternalURL(raw)
		assert.Error(t, err, "should reject in-cluster short form %s", raw)
	}
}

func TestValidateExternalURLRejectsServiceDNSWithTrailingDot(t *testing.T) {
	// FQDNs may end with a root dot. Ensure we still reject cluster-internal
	// hostnames even with a trailing dot.
	tests := []string{
		"http://hello.default.svc.cluster.local.",
		"https://hello.default.svc.",
	}
	for _, raw := range tests {
		err := ValidateExternalURL(raw)
		assert.Error(t, err, "should reject cluster-internal DNS even with trailing dot: %s", raw)
	}
}
