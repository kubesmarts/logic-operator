package v1

import (
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetCondition_InsertsNew(t *testing.T) {
	g := gomega.NewWithT(t)
	var conditions []metav1.Condition

	SetCondition(&conditions, ConditionDeploymentAvailable, metav1.ConditionTrue, 1, ReasonReady, "all good")

	g.Expect(conditions).To(gomega.HaveLen(1))
	g.Expect(conditions[0].Type).To(gomega.Equal(ConditionDeploymentAvailable))
	g.Expect(conditions[0].Status).To(gomega.Equal(metav1.ConditionTrue))
	g.Expect(conditions[0].Reason).To(gomega.Equal(ReasonReady))
	g.Expect(conditions[0].Message).To(gomega.Equal("all good"))
	g.Expect(conditions[0].ObservedGeneration).To(gomega.Equal(int64(1)))
	g.Expect(conditions[0].LastTransitionTime.IsZero()).To(gomega.BeFalse())
}

func TestSetCondition_UpdatesExisting(t *testing.T) {
	g := gomega.NewWithT(t)
	var conditions []metav1.Condition

	SetCondition(&conditions, ConditionDeploymentAvailable, metav1.ConditionFalse, 1, ReasonDeploymentProgressing, "")
	originalTime := conditions[0].LastTransitionTime

	SetCondition(&conditions, ConditionDeploymentAvailable, metav1.ConditionTrue, 2, ReasonReady, "ready now")

	g.Expect(conditions).To(gomega.HaveLen(1))
	g.Expect(conditions[0].Status).To(gomega.Equal(metav1.ConditionTrue))
	g.Expect(conditions[0].Reason).To(gomega.Equal(ReasonReady))
	g.Expect(conditions[0].ObservedGeneration).To(gomega.Equal(int64(2)))
	g.Expect(conditions[0].LastTransitionTime.Time).NotTo(gomega.Equal(originalTime.Time))
}

func TestSetCondition_SameStatusPreservesTransitionTime(t *testing.T) {
	g := gomega.NewWithT(t)
	var conditions []metav1.Condition

	SetCondition(&conditions, ConditionDeploymentAvailable, metav1.ConditionTrue, 1, ReasonReady, "")
	originalTime := conditions[0].LastTransitionTime

	SetCondition(&conditions, ConditionDeploymentAvailable, metav1.ConditionTrue, 2, ReasonReady, "still good")

	g.Expect(conditions[0].LastTransitionTime.Time).To(gomega.Equal(originalTime.Time))
	g.Expect(conditions[0].ObservedGeneration).To(gomega.Equal(int64(2)))
}

func TestSetCondition_MultipleTypes(t *testing.T) {
	g := gomega.NewWithT(t)
	var conditions []metav1.Condition

	SetCondition(&conditions, ConditionDeploymentAvailable, metav1.ConditionTrue, 1, ReasonReady, "")
	SetCondition(&conditions, ConditionServiceReady, metav1.ConditionFalse, 1, ReasonServiceNotFound, "")

	g.Expect(conditions).To(gomega.HaveLen(2))
	g.Expect(conditions[0].Type).To(gomega.Equal(ConditionDeploymentAvailable))
	g.Expect(conditions[1].Type).To(gomega.Equal(ConditionServiceReady))
}

func TestDerivePhase_AllTrueWithReplicas(t *testing.T) {
	g := gomega.NewWithT(t)
	conditions := []metav1.Condition{
		{Type: ConditionDeploymentAvailable, Status: metav1.ConditionTrue},
		{Type: ConditionServiceReady, Status: metav1.ConditionTrue},
	}

	g.Expect(DerivePhase(conditions, 2)).To(gomega.Equal(ApplicationPhaseReady))
}

func TestDerivePhase_AllTrueZeroReplicas(t *testing.T) {
	g := gomega.NewWithT(t)
	conditions := []metav1.Condition{
		{Type: ConditionDeploymentAvailable, Status: metav1.ConditionTrue},
		{Type: ConditionServiceReady, Status: metav1.ConditionTrue},
	}

	g.Expect(DerivePhase(conditions, 0)).To(gomega.Equal(ApplicationPhasePending))
}

func TestDerivePhase_DeploymentUnavailable(t *testing.T) {
	g := gomega.NewWithT(t)
	conditions := []metav1.Condition{
		{Type: ConditionDeploymentAvailable, Status: metav1.ConditionFalse, Reason: ReasonDeploymentProgressing},
		{Type: ConditionServiceReady, Status: metav1.ConditionTrue},
	}

	g.Expect(DerivePhase(conditions, 0)).To(gomega.Equal(ApplicationPhasePending))
}

func TestDerivePhase_ProgressDeadlineExceeded(t *testing.T) {
	g := gomega.NewWithT(t)
	conditions := []metav1.Condition{
		{Type: ConditionDeploymentAvailable, Status: metav1.ConditionFalse, Reason: ReasonProgressDeadlineExceeded},
		{Type: ConditionServiceReady, Status: metav1.ConditionTrue},
	}

	g.Expect(DerivePhase(conditions, 0)).To(gomega.Equal(ApplicationPhaseFailed))
}

func TestDerivePhase_NoConditions(t *testing.T) {
	g := gomega.NewWithT(t)

	g.Expect(DerivePhase(nil, 0)).To(gomega.Equal(ApplicationPhasePending))
	g.Expect(DerivePhase([]metav1.Condition{}, 0)).To(gomega.Equal(ApplicationPhasePending))
}
