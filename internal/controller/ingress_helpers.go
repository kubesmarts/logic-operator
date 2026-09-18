package controller

import (
	"context"
	"fmt"

	"github.com/kubesmarts/logic-operator/utils"

	routev1 "github.com/openshift/api/route/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveIngressURL resolves the external URL from an Ingress or Route resource.
// Returns empty string if the resource doesn't exist or host is not yet populated.
func ResolveIngressURL(ctx context.Context, c client.Client, objKey client.ObjectKey, tlsEnabled bool) (string, error) {
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	host, err := ResolveIngressHost(ctx, c, objKey)
	if err != nil {
		return "", err
	}
	if host == "" {
		return "", nil
	}

	return fmt.Sprintf("%s://%s", scheme, host), nil
}

// ResolveIngressHost resolves the hostname from an Ingress or Route resource.
// Platform detection determines whether to look for Route (OpenShift) or Ingress (Kubernetes).
func ResolveIngressHost(ctx context.Context, c client.Client, objKey client.ObjectKey) (string, error) {
	if utils.IsOpenShift() {
		return resolveHostFromRoute(ctx, c, objKey)
	}
	return resolveHostFromIngress(ctx, c, objKey)
}

func resolveHostFromRoute(ctx context.Context, c client.Client, objKey client.ObjectKey) (string, error) {
	var route routev1.Route
	err := c.Get(ctx, objKey, &route)
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	// Check status ingress for populated host (preferred)
	for _, ingress := range route.Status.Ingress {
		if ingress.Host != "" {
			return ingress.Host, nil
		}
	}

	// Fallback to spec host
	return route.Spec.Host, nil
}

func resolveHostFromIngress(ctx context.Context, c client.Client, objKey client.ObjectKey) (string, error) {
	var ingress networkingv1.Ingress
	err := c.Get(ctx, objKey, &ingress)
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	// Get host from rules (preferred)
	if len(ingress.Spec.Rules) > 0 && ingress.Spec.Rules[0].Host != "" {
		return ingress.Spec.Rules[0].Host, nil
	}

	// Fallback to LoadBalancer status
	for _, lb := range ingress.Status.LoadBalancer.Ingress {
		if lb.Hostname != "" {
			return lb.Hostname, nil
		}
		if lb.IP != "" {
			return lb.IP, nil
		}
	}

	return "", nil
}
