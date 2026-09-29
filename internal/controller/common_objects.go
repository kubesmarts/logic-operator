package controller

import (
	"fmt"
	"maps"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

const (
	FieldOwnerLogicOperator = "logic-operator"
	LabelManagedBy          = "logic-operator"
	LabelPartOf             = "logic-platform"

	LabelKeyName      = "app.kubernetes.io/name"
	LabelKeyInstance  = "app.kubernetes.io/instance"
	LabelKeyManagedBy = "app.kubernetes.io/managed-by"

	defaultPostgresPort = 5432
)

func ChildLabels(owner metav1.Object) map[string]string {
	labels := make(map[string]string)
	maps.Copy(labels, owner.GetLabels())
	labels[LabelKeyName] = owner.GetName()
	labels[LabelKeyManagedBy] = LabelManagedBy
	labels["app.kubernetes.io/part-of"] = LabelPartOf
	return labels
}

// ChildLabelsInstance returns the labels for an object that's not directly derived from the Logic CRDs and won't be named after it.
func ChildLabelsInstance(owner metav1.Object, app string) map[string]string {
	labels := make(map[string]string)
	maps.Copy(labels, owner.GetLabels())
	labels[LabelKeyName] = app
	labels[LabelKeyManagedBy] = LabelManagedBy
	labels["app.kubernetes.io/part-of"] = LabelPartOf
	labels[LabelKeyInstance] = owner.GetName()
	return labels
}

func SelectorLabels(name string) map[string]string {
	return map[string]string{
		LabelKeyName: name,
	}
}

func OwnerRef(owner metav1.Object, kind string) *metav1ac.OwnerReferenceApplyConfiguration {
	return metav1ac.OwnerReference().
		WithAPIVersion(logicv1.GroupVersion.String()).
		WithKind(kind).
		WithName(owner.GetName()).
		WithUID(owner.GetUID()).
		WithBlockOwnerDeletion(true).
		WithController(true)
}

// OwnerRefStandard creates a standard OwnerReference (not apply configuration).
// Use this for resources that don't have apply configurations (e.g., OpenShift Routes).
func OwnerRefStandard(owner metav1.Object, kind string) metav1.OwnerReference {
	isController := true
	return metav1.OwnerReference{
		APIVersion:         logicv1.GroupVersion.String(),
		Kind:               kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		Controller:         &isController,
		BlockOwnerDeletion: &isController,
	}
}

func MergeMaps(mapList ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range mapList {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// BuildPostgresAddress constructs a PostgreSQL connection address.
// If namespace is provided, treats svcName as a Kubernetes service and constructs the FQDN.
// If namespace is empty, treats svcName as an external hostname/FQDN and uses it as-is.
func BuildPostgresAddress(svcName, ns string, port int) string {
	var host string
	if ns != "" {
		// Kubernetes service: construct FQDN
		host = fmt.Sprintf("%s.%s.svc.cluster.local", svcName, ns)
	} else {
		// External hostname: use as-is (FQDN, hostname, or IP)
		host = svcName
	}

	if port == 0 {
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func envLiteral(name, value string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().WithName(name).WithValue(value)
}

func envFieldRef(name, fieldPath string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().
		WithName(name).
		WithValueFrom(corev1ac.EnvVarSource().
			WithFieldRef(corev1ac.ObjectFieldSelector().
				WithFieldPath(fieldPath)))
}

func envFromSecret(name, secretName, key string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().
		WithName(name).
		WithValueFrom(corev1ac.EnvVarSource().
			WithSecretKeyRef(corev1ac.SecretKeySelector().
				WithName(secretName).
				WithKey(key)))
}
