package controller

import (
	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
)

// probeTunings holds the platform probe timings. They are applied onto the operator default probes,
// never converted to corev1.Probe.
type probeTunings struct {
	Liveness  *logicv1.ProbeTuning
	Readiness *logicv1.ProbeTuning
	Startup   *logicv1.ProbeTuning
}

// flatContainer is the result of flattening the 3 tiers (podTemplate.container > container > top-level)
// for the fields that platform defaults can set.
type flatContainer struct {
	pullPolicy corev1.PullPolicy
	resources  corev1.ResourceRequirements
	env        []corev1.EnvVar
}

// mergeApplicationDefaults returns the effective ApplicationSpec for a runtime: runtime value, then platform
// value. Each spec is flattened across its tiers independently, so a platform container.* value never beats
// a runtime top-level value. The result is resolved by the existing functions (ToDeploymentSpec, ...).
// Neither input is modified, and the image and replicas are never taken from the platform.
//
//   - imagePullPolicy, resources: the platform value is used only when no runtime tier sets it.
//   - env: merged by name, the runtime wins; platform-only variables are placed before the runtime ones.
//   - pod labels and annotations: merged as maps, the runtime wins on a key conflict.
//
// The operator fallbacks (DefaultImagePullPolicy, default probes, ...) still apply afterwards.
func mergeApplicationDefaults(
	app *logicv1.ApplicationSpec,
	defaults *logicv1.ApplicationDefaultsSpec,
) (logicv1.ApplicationSpec, probeTunings, error) {
	effective := *app.DeepCopy()
	if defaults == nil {
		return effective, probeTunings{}, nil
	}

	defaultsApp, err := defaults.ToApplicationSpec()
	if err != nil {
		return effective, probeTunings{}, err
	}
	live, ready, startup := defaults.ProbeDefaults()
	tunings := probeTunings{Liveness: live, Readiness: ready, Startup: startup}

	appFlatContainer := flattenContainer(app)
	defaultsAppFlatContainer := flattenContainer(&defaultsApp)

	if appFlatContainer.pullPolicy == "" {
		effective.ImagePullPolicy = defaultsAppFlatContainer.pullPolicy
	}
	if !hasResources(appFlatContainer.resources) {
		effective.Resources = defaultsAppFlatContainer.resources
	}
	effective.Container.Env = append(platformOnlyEnv(defaultsAppFlatContainer.env, app), effective.Container.Env...)

	if defaultsAppMetadata := defaultsApp.PodTemplate.Metadata; defaultsAppMetadata != nil {
		var appLabels, appAnnotations map[string]string
		if appMetadata := app.PodTemplate.Metadata; appMetadata != nil {
			appLabels, appAnnotations = appMetadata.Labels, appMetadata.Annotations
		}
		effective.PodTemplate.Metadata = &logicv1.PodTemplateMetadata{
			Labels:      mergeMapsOrNil(defaultsAppMetadata.Labels, appLabels),
			Annotations: mergeMapsOrNil(defaultsAppMetadata.Annotations, appAnnotations),
		}
	}

	return effective, tunings, nil
}

// flattenContainer applies podTemplate.container > container > top-level.
// Resources are replaced whole by the highest tier that sets any of them. Env is merged by name.
func flattenContainer(app *logicv1.ApplicationSpec) flatContainer {
	fc := flatContainer{pullPolicy: app.ImagePullPolicy, resources: app.Resources}
	for _, cs := range []*logicv1.ContainerSpec{&app.Container, &app.PodTemplate.Container} {
		if cs.ImagePullPolicy != "" {
			fc.pullPolicy = cs.ImagePullPolicy
		}
		if hasResources(cs.Resources) {
			fc.resources = cs.Resources
		}
		fc.env = mergeEnvByName(fc.env, cs.Env)
	}
	return fc
}

// hasResources reports whether any field is set. Unlike applyContainerFields, claims count as set.
func hasResources(r corev1.ResourceRequirements) bool {
	return len(r.Requests) > 0 || len(r.Limits) > 0 || len(r.Claims) > 0
}

// mergeEnvByName returns base with overlay applied: same name replaces in place, new names are appended.
func mergeEnvByName(base, overlay []corev1.EnvVar) []corev1.EnvVar {
	result := append([]corev1.EnvVar(nil), base...)
	for _, e := range overlay {
		replaced := false
		for i := range result {
			if result[i].Name == e.Name {
				result[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, e)
		}
	}
	return result
}

// platformOnlyEnv returns the platform variables whose name is not defined in any runtime env tier.
func platformOnlyEnv(platformEnv []corev1.EnvVar, app *logicv1.ApplicationSpec) []corev1.EnvVar {
	runtimeNames := make(map[string]struct{})
	for _, e := range app.Container.Env {
		runtimeNames[e.Name] = struct{}{}
	}
	for _, e := range app.PodTemplate.Container.Env {
		runtimeNames[e.Name] = struct{}{}
	}

	var inherited []corev1.EnvVar
	for _, e := range platformEnv {
		if _, ok := runtimeNames[e.Name]; !ok {
			inherited = append(inherited, e)
		}
	}
	return inherited
}

// mergeMapsOrNil merges the maps (later wins) and returns nil when the result is empty.
func mergeMapsOrNil(maps ...map[string]string) map[string]string {
	merged := MergeMaps(maps...)
	if len(merged) == 0 {
		return nil
	}
	return merged
}
