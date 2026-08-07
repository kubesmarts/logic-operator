/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/open-workflow-specification/sdk-go/v4/model"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// LogicFlowDefinitionReconciler reconciles a LogicFlowDefinition object
type LogicFlowDefinitionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/finalizers,verbs=update
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *LogicFlowDefinitionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var def logicv1.LogicFlowDefinition
	if err := r.Get(ctx, req.NamespacedName, &def); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate runtimeRef exists
	var rt logicv1.LogicFlowRuntime
	rtKey := client.ObjectKey{Name: def.Spec.RuntimeRef.Name, Namespace: def.Namespace}
	if err := r.Get(ctx, rtKey, &rt); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("referenced LogicFlowRuntime not found", "runtimeRef", def.Spec.RuntimeRef.Name)
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRefValid, metav1.ConditionFalse, def.Generation, logicv1.ReasonRuntimeNotFound, fmt.Sprintf("LogicFlowRuntime %q not found", def.Spec.RuntimeRef.Name))
			return ctrl.Result{}, r.Status().Update(ctx, &def)
		}
		return ctrl.Result{}, err
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRefValid, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")

	// Parse flow document
	wf, err := def.Spec.ParseFlow()
	if err != nil {
		log.Error(err, "failed to parse flow document")
		logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionFlowParsed, metav1.ConditionFalse, def.Generation, logicv1.ReasonParseError, err.Error())
		return ctrl.Result{}, r.Status().Update(ctx, &def)
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionFlowParsed, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")

	if err := r.applyConfigMap(ctx, &def, wf); err != nil {
		log.Error(err, "failed to apply ConfigMap")
		logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionConfigMapReady, metav1.ConditionFalse, def.Generation, logicv1.ReasonSSAApplyFailed, err.Error())
		return ctrl.Result{}, r.Status().Update(ctx, &def)
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionConfigMapReady, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")

	if err := r.updateStatus(ctx, &def, wf); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func configMapName(def *logicv1.LogicFlowDefinition) string {
	return ConfigMapPrefix + def.Name
}

func (r *LogicFlowDefinitionReconciler) applyConfigMap(ctx context.Context, def *logicv1.LogicFlowDefinition, wf *model.Workflow) error {
	dataKey := wf.Document.Name + ".json"

	childLabels := ChildLabels(def)
	childLabels[LabelRuntimeRef] = def.Spec.RuntimeRef.Name
	childLabels[LabelWorkflowName] = wf.Document.Name
	childLabels[LabelWorkflowVersion] = wf.Document.Version

	cm := corev1ac.ConfigMap(configMapName(def), def.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(def, logicv1.LogicFlowDefinitionKind)).
		WithData(map[string]string{
			dataKey: string(def.Spec.Flow.Raw),
		})

	return r.Apply(ctx, cm, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowDefinitionReconciler) updateStatus(ctx context.Context, def *logicv1.LogicFlowDefinition, wf *model.Workflow) error {
	def.Status.ObservedGeneration = def.Generation
	def.Status.WorkflowName = wf.Document.Name
	def.Status.WorkflowVersion = wf.Document.Version
	def.Status.WorkflowNamespace = wf.Document.Namespace
	def.Status.ConfigMapRef = &corev1.LocalObjectReference{Name: configMapName(def)}

	return r.Status().Update(ctx, def)
}

func (r *LogicFlowDefinitionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicFlowDefinition{}).
		Owns(&corev1.ConfigMap{}).
		Named("logicflowdefinition").
		Complete(r)
}
