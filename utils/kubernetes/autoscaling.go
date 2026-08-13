/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package kubernetes

import (
	"context"
	"reflect"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FindHPAForDeployment returns the HorizontalPodAutoscaler targeting a deployment in a given namespace, or nil if it
// doesn't exist.
// Note: By k8s definition, the HorizontalPodAutoscaler must belong to the same namespace as the managed deployment.
func FindHPAForDeployment(ctx context.Context, c client.Client, namespace string, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	return findHPAForTarget(ctx, c, namespace, "apps/v1", "Application", name)
}

// FindHPAForLogicFlowRuntime returns the HorizontalPodAutoscaler targeting a LogicFlowRuntime in a given namespace, or nil if it
// doesn't exist.
// Note: By k8s definition, the HorizontalPodAutoscaler must belong to the same namespace as the managed runtime.
func FindHPAForLogicFlowRuntime(ctx context.Context, c client.Client, namespace string, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	return findHPAForTarget(ctx, c, namespace, "logic.kubesmarts.org/v1", "LogicFlowRuntime", name)
}

func findHPAForTarget(ctx context.Context, c client.Client, namespace string, apiVersion string, kind string, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	var hpaList autoscalingv2.HorizontalPodAutoscalerList
	if err := c.List(ctx, &hpaList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	for _, hpa := range hpaList.Items {
		ref := hpa.Spec.ScaleTargetRef
		if ref.Kind == kind && ref.Name == name && ref.APIVersion == apiVersion {
			return &hpa, nil
		}
	}
	return nil, nil
}

// HPAIsActive returns true if the HorizontalPodAutoscaler is active.
func HPAIsActive(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, cond := range hpa.Status.Conditions {
		if cond.Type == autoscalingv2.ScalingActive {
			return cond.Status == v1.ConditionTrue
		}
	}
	return false
}

// HPAIsWorking returns true if the HorizontalPodAutoscaler has started to take care of the scaling for the
// corresponding target ref. At this point, our controllers must transfer the control to the HorizontalPodAutoscaler
// and let it manage the replicas.
func HPAIsWorking(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	return HPAIsActive(hpa) || hpa.Status.DesiredReplicas > 0
}

// HPAEqualsBySpec returns true if to HorizontalPodAutoscaler has the same Spec, false in any other case.
func HPAEqualsBySpec(hpa1, hpa2 *autoscalingv2.HorizontalPodAutoscaler) bool {
	var hpa1Spec *autoscalingv2.HorizontalPodAutoscalerSpec
	var hpa2Spec *autoscalingv2.HorizontalPodAutoscalerSpec
	if hpa1 != nil {
		hpa1Spec = &hpa1.Spec
	}
	if hpa2 != nil {
		hpa2Spec = &hpa2.Spec
	}
	return reflect.DeepEqual(hpa1Spec, hpa2Spec)
}

// IsHPAndTargetsAKind returns (*autoscalingv2.HorizontalPodAutoscaler, true) if the object is a HorizontalPodAutoscaler
// and targets a given kind, (nil, false) in other cases.
func IsHPAndTargetsAKind(obj client.Object, kind string) (*autoscalingv2.HorizontalPodAutoscaler, bool) {
	if hpa, ok := obj.(*autoscalingv2.HorizontalPodAutoscaler); ok {
		if hpa != nil && hpa.Spec.ScaleTargetRef.Kind == kind {
			return hpa, true
		}
	}
	return nil, false
}

// IsHPAndTargetsADeployment returns (*autoscalingv2.HorizontalPodAutoscaler, true) if the object is a HorizontalPodAutoscaler
// and targets a Application, (nil, false) in other cases.
func IsHPAndTargetsADeployment(obj client.Object) (*autoscalingv2.HorizontalPodAutoscaler, bool) {
	return IsHPAndTargetsAKind(obj, "Application")
}

// IsHPAndTargetsALogicFlowRuntime returns (*autoscalingv2.HorizontalPodAutoscaler, true) if the object is a HorizontalPodAutoscaler
// and targets a LogicFlowRuntime, (nil, false) in other cases.
func IsHPAndTargetsALogicFlowRuntime(obj client.Object) (*autoscalingv2.HorizontalPodAutoscaler, bool) {
	return IsHPAndTargetsAKind(obj, "LogicFlowRuntime")
}

// IsHPAndTargetsALogicFlowRuntimeAsBool returns true if the object is a HorizontalPodAutoscaler and targets a LogicFlowRuntime,
// false in other cases.
func IsHPAndTargetsALogicFlowRuntimeAsBool(obj client.Object) bool {
	_, ok := IsHPAndTargetsAKind(obj, "LogicFlowRuntime")
	return ok
}

// HPAMinReplicasIsGreaterThan returns true if the HorizontalPodAutoscaler configured minReplicas is != nil, and greater
// than the given value. False in any other case.
func HPAMinReplicasIsGreaterThan(hpa *autoscalingv2.HorizontalPodAutoscaler, value int32) bool {
	return hpa.Spec.MinReplicas != nil && *hpa.Spec.MinReplicas > value
}
