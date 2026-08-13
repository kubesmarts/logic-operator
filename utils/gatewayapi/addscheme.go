package gatewayapi

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// MustAddToScheme adds Gateway API types to the given scheme or panics.
func MustAddToScheme(s *runtime.Scheme) {
	utilruntime.Must(gatewayv1.Install(s))
}
