package controller

import (
	"fmt"
	"strconv"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	routev1 "github.com/openshift/api/route/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1ac "sigs.k8s.io/gateway-api/applyconfiguration/apis/v1"
)

const (
	flowExecPathPrefix = "/q/flow/exec"

	annotationCertManagerIssuer          = "cert-manager.io/cluster-issuer"
	annotationCertManagerNamespaceIssuer = "cert-manager.io/issuer"

	annotationNginxRewriteTarget = "nginx.ingress.kubernetes.io/rewrite-target"
	annotationNginxUseRegex      = "nginx.ingress.kubernetes.io/use-regex"
	annotationNginxCanary        = "nginx.ingress.kubernetes.io/canary"
	annotationNginxCanaryWeight  = "nginx.ingress.kubernetes.io/canary-weight"

	annotationHAProxyRewriteTarget = "haproxy.router.openshift.io/rewrite-target"
)

type trafficTarget struct {
	version string
	weight  int32
}

func quarkusFlowPath(wfNamespace, wfName, version string) string {
	if version == "" {
		return fmt.Sprintf("%s/%s/%s", flowExecPathPrefix, wfNamespace, wfName)
	}
	return fmt.Sprintf("%s/%s/%s/%s", flowExecPathPrefix, wfNamespace, wfName, version)
}

// ingressForService creates a standard Ingress with nginx rewrite-target.
// Used for DefaultDefinition or as the primary Ingress in traffic splitting.
func ingressForService(
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	wfNamespace, wfName, version string,
) *networkingv1ac.IngressApplyConfiguration {
	pathType := networkingv1.PathTypePrefix
	rewriteTarget := quarkusFlowPath(wfNamespace, wfName, version)

	annotations := map[string]string{
		annotationNginxRewriteTarget: rewriteTarget,
	}
	for k, v := range svc.Spec.Ingress.Annotations {
		annotations[k] = v
	}
	applyCertManagerAnnotations(svc, annotations)

	rule := networkingv1ac.IngressRule().
		WithHTTP(networkingv1ac.HTTPIngressRuleValue().
			WithPaths(networkingv1ac.HTTPIngressPath().
				WithPath("/").
				WithPathType(pathType).
				WithBackend(networkingv1ac.IngressBackend().
					WithService(networkingv1ac.IngressServiceBackend().
						WithName(rt.Name).
						WithPort(networkingv1ac.ServiceBackendPort().
							WithNumber(defaultPort))))))

	if svc.Spec.Ingress.Host != "" {
		rule = rule.WithHost(svc.Spec.Ingress.Host)
	}

	spec := networkingv1ac.IngressSpec().WithRules(rule)
	if svc.Spec.Ingress.IngressClassName != nil {
		spec = spec.WithIngressClassName(*svc.Spec.Ingress.IngressClassName)
	}
	applyIngressTLS(svc, spec)

	return networkingv1ac.Ingress(svc.Name, svc.Namespace).
		WithLabels(ChildLabels(svc)).
		WithAnnotations(annotations).
		WithOwnerReferences(OwnerRef(svc, logicv1.LogicFlowServiceKind)).
		WithSpec(spec)
}

// canaryIngressForService creates a canary Ingress for nginx traffic splitting.
func canaryIngressForService(
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	wfNamespace, wfName, version string,
	weight int32,
) *networkingv1ac.IngressApplyConfiguration {
	pathType := networkingv1.PathTypePrefix
	rewriteTarget := quarkusFlowPath(wfNamespace, wfName, version)

	annotations := map[string]string{
		annotationNginxRewriteTarget: rewriteTarget,
		annotationNginxCanary:        "true",
		annotationNginxCanaryWeight:  strconv.FormatInt(int64(weight), 10),
	}
	for k, v := range svc.Spec.Ingress.Annotations {
		annotations[k] = v
	}

	rule := networkingv1ac.IngressRule().
		WithHTTP(networkingv1ac.HTTPIngressRuleValue().
			WithPaths(networkingv1ac.HTTPIngressPath().
				WithPath("/").
				WithPathType(pathType).
				WithBackend(networkingv1ac.IngressBackend().
					WithService(networkingv1ac.IngressServiceBackend().
						WithName(rt.Name).
						WithPort(networkingv1ac.ServiceBackendPort().
							WithNumber(defaultPort))))))

	if svc.Spec.Ingress.Host != "" {
		rule = rule.WithHost(svc.Spec.Ingress.Host)
	}

	spec := networkingv1ac.IngressSpec().WithRules(rule)
	if svc.Spec.Ingress.IngressClassName != nil {
		spec = spec.WithIngressClassName(*svc.Spec.Ingress.IngressClassName)
	}
	applyIngressTLS(svc, spec)

	name := svc.Name + "-canary"
	return networkingv1ac.Ingress(name, svc.Namespace).
		WithLabels(ChildLabels(svc)).
		WithAnnotations(annotations).
		WithOwnerReferences(OwnerRef(svc, logicv1.LogicFlowServiceKind)).
		WithSpec(spec)
}

// directVersionIngress creates an Ingress with regex path for direct version access (/v/{version}).
func directVersionIngress(
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	wfNamespace, wfName string,
) *networkingv1ac.IngressApplyConfiguration {
	pathType := networkingv1.PathTypeImplementationSpecific
	rewriteTarget := fmt.Sprintf("%s/%s/%s/$1", flowExecPathPrefix, wfNamespace, wfName)

	annotations := map[string]string{
		annotationNginxRewriteTarget: rewriteTarget,
		annotationNginxUseRegex:      "true",
	}
	for k, v := range svc.Spec.Ingress.Annotations {
		annotations[k] = v
	}

	rule := networkingv1ac.IngressRule().
		WithHTTP(networkingv1ac.HTTPIngressRuleValue().
			WithPaths(networkingv1ac.HTTPIngressPath().
				WithPath("/v/(.+)").
				WithPathType(pathType).
				WithBackend(networkingv1ac.IngressBackend().
					WithService(networkingv1ac.IngressServiceBackend().
						WithName(rt.Name).
						WithPort(networkingv1ac.ServiceBackendPort().
							WithNumber(defaultPort))))))

	if svc.Spec.Ingress.Host != "" {
		rule = rule.WithHost(svc.Spec.Ingress.Host)
	}

	spec := networkingv1ac.IngressSpec().WithRules(rule)
	if svc.Spec.Ingress.IngressClassName != nil {
		spec = spec.WithIngressClassName(*svc.Spec.Ingress.IngressClassName)
	}
	applyIngressTLS(svc, spec)

	name := svc.Name + "-direct"
	return networkingv1ac.Ingress(name, svc.Namespace).
		WithLabels(ChildLabels(svc)).
		WithAnnotations(annotations).
		WithOwnerReferences(OwnerRef(svc, logicv1.LogicFlowServiceKind)).
		WithSpec(spec)
}

// routeForDefault creates an OpenShift Route with haproxy rewrite for single-version access.
func routeForDefault(
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	wfNamespace, wfName, version string,
) *routev1.Route {
	weight := int32(100)
	rewriteTarget := quarkusFlowPath(wfNamespace, wfName, version)

	annotations := map[string]string{
		annotationHAProxyRewriteTarget: rewriteTarget,
	}
	for k, v := range svc.Spec.Ingress.Annotations {
		annotations[k] = v
	}

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svc.Name,
			Namespace:   svc.Namespace,
			Labels:      ChildLabels(svc),
			Annotations: annotations,
			OwnerReferences: []metav1.OwnerReference{
				ownerRefStandard(svc),
			},
		},
		Spec: routev1.RouteSpec{
			Path: "/",
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   rt.Name,
				Weight: &weight,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt32(defaultPort),
			},
		},
	}

	if svc.Spec.Ingress.Host != "" {
		route.Spec.Host = svc.Spec.Ingress.Host
	}

	if svc.Spec.Ingress.TLS.Enabled {
		route.Spec.TLS = &routev1.TLSConfig{
			Termination:                   routev1.TLSTerminationEdge,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		}
	}

	return route
}

// httpRouteForService creates a Gateway API HTTPRoute with per-backend weights and path rewrites.
func httpRouteForService(
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	wfNamespace, wfName string,
	targets []trafficTarget,
) *gatewayv1ac.HTTPRouteApplyConfiguration {
	gw := svc.Spec.Ingress.GatewayRef
	parentRef := gatewayv1ac.ParentReference().
		WithName(gatewayv1.ObjectName(gw.Name))
	if gw.Namespace != nil {
		parentRef = parentRef.WithNamespace(gatewayv1.Namespace(*gw.Namespace))
	}

	backendRefs := make([]*gatewayv1ac.HTTPBackendRefApplyConfiguration, 0, len(targets))
	for _, t := range targets {
		rewritePath := quarkusFlowPath(wfNamespace, wfName, t.version)
		ref := gatewayv1ac.HTTPBackendRef().
			WithName(gatewayv1.ObjectName(rt.Name)).
			WithPort(defaultPort).
			WithWeight(t.weight).
			WithFilters(gatewayv1ac.HTTPRouteFilter().
				WithType(gatewayv1.HTTPRouteFilterURLRewrite).
				WithURLRewrite(gatewayv1ac.HTTPURLRewriteFilter().
					WithPath(gatewayv1ac.HTTPPathModifier().
						WithType(gatewayv1.PrefixMatchHTTPPathModifier).
						WithReplacePrefixMatch(rewritePath))))
		backendRefs = append(backendRefs, ref)
	}

	weightedRule := gatewayv1ac.HTTPRouteRule().
		WithMatches(gatewayv1ac.HTTPRouteMatch().
			WithPath(gatewayv1ac.HTTPPathMatch().
				WithType(gatewayv1.PathMatchPathPrefix).
				WithValue("/"))).
		WithBackendRefs(backendRefs...)

	directRule := gatewayv1ac.HTTPRouteRule().
		WithMatches(gatewayv1ac.HTTPRouteMatch().
			WithPath(gatewayv1ac.HTTPPathMatch().
				WithType(gatewayv1.PathMatchPathPrefix).
				WithValue("/v/"))).
		WithBackendRefs(gatewayv1ac.HTTPBackendRef().
			WithName(gatewayv1.ObjectName(rt.Name)).
			WithPort(defaultPort).
			WithFilters(gatewayv1ac.HTTPRouteFilter().
				WithType(gatewayv1.HTTPRouteFilterURLRewrite).
				WithURLRewrite(gatewayv1ac.HTTPURLRewriteFilter().
					WithPath(gatewayv1ac.HTTPPathModifier().
						WithType(gatewayv1.PrefixMatchHTTPPathModifier).
						WithReplacePrefixMatch(quarkusFlowPath(wfNamespace, wfName, ""))))))

	spec := gatewayv1ac.HTTPRouteSpec().
		WithParentRefs(parentRef).
		WithRules(directRule, weightedRule)

	if svc.Spec.Ingress.Host != "" {
		spec = spec.WithHostnames(gatewayv1.Hostname(svc.Spec.Ingress.Host))
	}

	return gatewayv1ac.HTTPRoute(svc.Name, svc.Namespace).
		WithLabels(ChildLabels(svc)).
		WithAnnotations(svc.Spec.Ingress.Annotations).
		WithOwnerReferences(OwnerRef(svc, logicv1.LogicFlowServiceKind)).
		WithSpec(spec)
}

func applyCertManagerAnnotations(svc *logicv1.LogicFlowService, annotations map[string]string) {
	if !svc.Spec.Ingress.TLS.Enabled || svc.Spec.Ingress.TLS.CertManager == nil {
		return
	}
	cm := svc.Spec.Ingress.TLS.CertManager
	if cm.IssuerRef.Kind == "ClusterIssuer" || cm.IssuerRef.Kind == "" {
		annotations[annotationCertManagerIssuer] = cm.IssuerRef.Name
	} else {
		annotations[annotationCertManagerNamespaceIssuer] = cm.IssuerRef.Name
	}
}

func applyIngressTLS(svc *logicv1.LogicFlowService, spec *networkingv1ac.IngressSpecApplyConfiguration) {
	if !svc.Spec.Ingress.TLS.Enabled {
		return
	}
	tlsSpec := networkingv1ac.IngressTLS()
	if svc.Spec.Ingress.Host != "" {
		tlsSpec = tlsSpec.WithHosts(svc.Spec.Ingress.Host)
	}
	if svc.Spec.Ingress.TLS.SecretRef.Name != "" {
		tlsSpec = tlsSpec.WithSecretName(svc.Spec.Ingress.TLS.SecretRef.Name)
	} else if svc.Spec.Ingress.TLS.CertManager != nil {
		tlsSpec = tlsSpec.WithSecretName(svc.Name + "-tls")
	}
	spec.WithTLS(tlsSpec)
}

func ownerRefStandard(owner metav1.Object) metav1.OwnerReference {
	isController := true
	return metav1.OwnerReference{
		APIVersion:         logicv1.GroupVersion.String(),
		Kind:               logicv1.LogicFlowServiceKind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		Controller:         &isController,
		BlockOwnerDeletion: &isController,
	}
}
