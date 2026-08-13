package controller

import (
	"testing"

	"github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

func TestChildLabels(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("propagates CR labels with operator labels", func(t *testing.T) {
		g = gomega.NewWithT(t)
		rt := &logicv1.LogicFlowRuntime{}
		rt.Name = testRuntimeName
		rt.Labels = map[string]string{testLabelTeam: testPlatformLabel}

		labels := ChildLabels(rt)
		g.Expect(labels).To(gomega.HaveKeyWithValue(testLabelKeyName, testRuntimeName))
		g.Expect(labels).To(gomega.HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
		g.Expect(labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/part-of", LabelPartOf))
		g.Expect(labels).To(gomega.HaveKeyWithValue(testLabelTeam, testPlatformLabel))
	})

	t.Run("operator labels override CR labels", func(t *testing.T) {
		g = gomega.NewWithT(t)
		rt := &logicv1.LogicFlowRuntime{}
		rt.Name = testRuntimeName
		rt.Labels = map[string]string{testLabelKeyManagedBy: "user-override"}

		labels := ChildLabels(rt)
		g.Expect(labels).To(gomega.HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
	})
}

func TestSelectorLabels(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels(testRuntimeName)
	g.Expect(sel).To(gomega.HaveLen(2))
	g.Expect(sel).To(gomega.HaveKeyWithValue(testLabelKeyName, testRuntimeName))
	g.Expect(sel).To(gomega.HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
}
