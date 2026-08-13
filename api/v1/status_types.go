package v1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=Pending;Ready;Failed
type ApplicationPhase string

const (
	ApplicationPhasePending ApplicationPhase = "Pending"
	ApplicationPhaseReady   ApplicationPhase = "Ready"
	ApplicationPhaseFailed  ApplicationPhase = "Failed"
)

const (
	ConditionDeploymentAvailable = "DeploymentAvailable"
	ConditionServiceReady        = "ServiceReady"
	ConditionLeaseReady          = "LeaseReady"
)

const (
	ReasonDeploymentNotFound       = "DeploymentNotFound"
	ReasonServiceNotFound          = "ServiceNotFound"
	ReasonLeaseNotFound            = "LeaseNotFound"
	ReasonReady                    = "Ready"
	ReasonDeploymentProgressing    = "DeploymentProgressing"
	ReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
)

const (
	ConditionRuntimeRefValid   = "RuntimeRefValid"
	ConditionFlowParsed        = "FlowParsed"
	ConditionConfigMapReady    = "ConfigMapReady"
	ConditionRuntimeConsistent = "RuntimeConsistent"
)

const (
	ReasonRuntimeNotFound = "RuntimeNotFound"
	ReasonRuntimeConflict = "RuntimeConflict"
	ReasonParseError      = "ParseError"
	ReasonSSAApplyFailed  = "ReasonSSAApplyFailed"
)

const (
	ConditionDefinitionsReady = "DefinitionsReady"
	ConditionIngressReady     = "IngressReady"
	ConditionTLSReady         = "TLSReady"
)

const (
	ReasonGatewayRefRequired   = "GatewayRefRequired"
	ReasonIngressMisconfigured = "IngressMisconfigured"
)

// SetCondition sets a condition on a Conditions slice, handling insert/update
// and LastTransitionTime (only updated when status actually changes).
func SetCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, observedGeneration int64, reason, message string) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: observedGeneration,
		Reason:             reason,
		Message:            message,
	})
}

// DerivePhase computes the ApplicationPhase from conditions and ready replica count.
func DerivePhase(conditions []metav1.Condition, readyReplicas int32) ApplicationPhase {
	phase := ApplicationPhaseReady
	for _, cond := range conditions {
		if cond.Status == metav1.ConditionFalse {
			phase = ApplicationPhasePending
			if cond.Reason == ReasonProgressDeadlineExceeded {
				return ApplicationPhaseFailed
			}
		}
	}
	if phase == ApplicationPhaseReady && readyReplicas == 0 {
		return ApplicationPhasePending
	}
	return phase
}
