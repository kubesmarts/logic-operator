package controller

import (
	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

const (
	FieldOwnerLogicOperator = "logic-operator"
	LabelManagedBy          = "logic-operator"
	LabelPartOf             = "logic-platform"

	LabelKeyName      = "app.kubernetes.io/name"
	LabelKeyManagedBy = "app.kubernetes.io/managed-by"
)

func ChildLabels(owner metav1.Object) map[string]string {
	labels := make(map[string]string)
	for k, v := range owner.GetLabels() {
		labels[k] = v
	}
	labels[LabelKeyName] = owner.GetName()
	labels[LabelKeyManagedBy] = LabelManagedBy
	labels["app.kubernetes.io/part-of"] = LabelPartOf
	return labels
}

func SelectorLabels(name string) map[string]string {
	return map[string]string{
		LabelKeyName:      name,
		LabelKeyManagedBy: LabelManagedBy,
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

func MergeMaps(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}
