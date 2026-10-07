package discovery

import (
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestStripDefinitionSpecDropsFlow(t *testing.T) {
	def := &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-def",
			Labels:    map[string]string{logicv1.LabelWorkflowName: "hello"},
		},
		Spec: logicv1.LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: "rt1"},
			Flow:       runtime.RawExtension{Raw: []byte(`{"huge":"flow document"}`)},
		},
	}

	out, err := stripDefinitionSpec(def)
	require.NoError(t, err)

	stripped := out.(*logicv1.LogicFlowDefinition)
	assert.Equal(t, logicv1.LogicFlowDefinitionSpec{}, stripped.Spec, "spec should be zeroed")
	assert.Equal(t, "hello", stripped.Labels[logicv1.LabelWorkflowName], "labels must be preserved")
}

func TestStripDefinitionSpecPassesThroughOtherTypes(t *testing.T) {
	svc := &logicv1.LogicFlowService{}
	out, err := stripDefinitionSpec(svc)
	require.NoError(t, err)
	assert.Same(t, svc, out)
}
