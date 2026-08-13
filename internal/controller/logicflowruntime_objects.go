package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

const (
	ContainerNameRunner = "logic-runner"
)

func durableDeploymentStrategy(replicas int32) *appsv1ac.DeploymentStrategyApplyConfiguration {
	if replicas <= 1 {
		return appsv1ac.DeploymentStrategy().
			WithType(appsv1.RecreateDeploymentStrategyType)
	}
	one := intstr.FromInt32(1)
	return appsv1ac.DeploymentStrategy().
		WithType(appsv1.RollingUpdateDeploymentStrategyType).
		WithRollingUpdate(appsv1ac.RollingUpdateDeployment().
			WithMaxUnavailable(one).
			WithMaxSurge(one))
}

func memberLeaseLabels(poolName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": DurableManagedByValue,
		"app.kubernetes.io/component":  DurableComponentValue,
		LabelDurablePool:               poolName,
		LabelDurableIsLeader:           "false",
	}
}

func newMemberLease(name, namespace, poolName string, dep *appsv1.Deployment) *coordinationv1.Lease {
	duration := LeaseDuration
	controller := false
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    memberLeaseLabels(poolName),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       dep.Name,
					UID:        dep.UID,
					Controller: &controller,
				},
			},
		},
		Spec: coordinationv1.LeaseSpec{
			LeaseDurationSeconds: &duration,
		},
	}
}

// FlowVolumes returns pod-level Volume entries for ConfigMaps.
func FlowVolumes(configMaps []corev1.ConfigMap) []*corev1ac.VolumeApplyConfiguration {
	vols := make([]*corev1ac.VolumeApplyConfiguration, 0, len(configMaps))
	for i := range configMaps {
		vols = append(vols, corev1ac.Volume().
			WithName(configMaps[i].Name).
			WithConfigMap(corev1ac.ConfigMapVolumeSource().
				WithName(configMaps[i].Name)))
	}
	return vols
}
