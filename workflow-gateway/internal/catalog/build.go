package catalog

import (
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
)

// definitionResolver fetches a LogicFlowDefinition by name within a namespace.
type definitionResolver func(namespace, name string) (*logicv1.LogicFlowDefinition, error)

// BuildExecutionEndpoints constructs the execution endpoint map by following each
// Service's references to the Definitions it exposes. The routing key comes from
// the Definition's labels; the URL from the Service status. Returns the map and
// a slice of non-fatal errors.
func BuildExecutionEndpoints(
	services []logicv1.LogicFlowService,
	resolve definitionResolver,
) (map[routing.WorkflowKey]routing.URL, []error) {
	endpoints := make(map[routing.WorkflowKey]routing.URL)
	var errs []error

	for i := range services {
		svc := &services[i]
		if svc.Status.URL == "" {
			continue
		}
		if err := ValidateExternalURL(svc.Status.URL); err != nil {
			errs = append(errs, fmt.Errorf("service %s/%s has invalid URL %q: %w", svc.Namespace, svc.Name, svc.Status.URL, err))
			continue
		}
		for _, name := range referencedDefinitions(svc) {
			def, err := resolve(svc.Namespace, name)
			if err != nil {
				errs = append(errs, fmt.Errorf("service %s/%s references definition %q: %w", svc.Namespace, svc.Name, name, err))
				continue
			}
			key, err := workflowKeyFromLabels(def)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if existing, isDuplicate := endpoints[key]; isDuplicate && existing != routing.URL(svc.Status.URL) {
				errs = append(errs, fmt.Errorf("duplicate workflow key %v: service %s/%s would overwrite existing endpoint to %s with %s", key, svc.Namespace, svc.Name, existing, svc.Status.URL))
				continue
			}
			endpoints[key] = routing.URL(svc.Status.URL)
		}
	}
	return endpoints, errs
}

// BuildRuntimeServiceEndpoints maps each LogicFlowDefinition directly to the
// Kubernetes Service created for its referenced LogicFlowRuntime. These routes
// let the gateway serve workflows that do not have a LogicFlowService; a
// configured LogicFlowService URL can override them in the watcher.
func BuildRuntimeServiceEndpoints(
	definitions []logicv1.LogicFlowDefinition,
) (map[routing.WorkflowKey]routing.URL, []error) {
	endpoints := make(map[routing.WorkflowKey]routing.URL, len(definitions))
	var errs []error

	for i := range definitions {
		def := &definitions[i]
		key, err := workflowKeyFromLabels(def)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		runtimeName := def.Spec.RuntimeRef.Name
		if runtimeName == "" {
			errs = append(errs, fmt.Errorf("definition %s/%s has no runtimeRef.name", def.Namespace, def.Name))
			continue
		}

		// Runtime Services use port 80 and share the LogicFlowRuntime's name and
		// namespace. Kubernetes DNS search resolves service.namespace against
		// the cluster service domain without assuming that domain's value.
		endpoint := routing.URL(fmt.Sprintf("http://%s.%s", runtimeName, def.Namespace))
		if existing, duplicate := endpoints[key]; duplicate && existing != endpoint {
			errs = append(errs, fmt.Errorf("duplicate workflow key %v: definition %s/%s targets %s, already mapped to %s", key, def.Namespace, def.Name, endpoint, existing))
			continue
		}
		endpoints[key] = endpoint
	}

	return endpoints, errs
}

func referencedDefinitions(svc *logicv1.LogicFlowService) []string {
	if svc.Spec.DefaultDefinition != nil && svc.Spec.DefaultDefinition.Name != "" {
		return []string{svc.Spec.DefaultDefinition.Name}
	}
	names := make([]string, 0, len(svc.Spec.Traffic))
	for i := range svc.Spec.Traffic {
		if name := svc.Spec.Traffic[i].DefinitionRef.Name; name != "" {
			names = append(names, name)
		}
	}
	return names
}

func workflowKeyFromLabels(def *logicv1.LogicFlowDefinition) (routing.WorkflowKey, error) {
	ns := def.Labels[logicv1.LabelWorkflowNamespace]
	name := def.Labels[logicv1.LabelWorkflowName]
	version := def.Labels[logicv1.LabelWorkflowVersion]
	if ns == "" || name == "" || version == "" {
		return routing.WorkflowKey{}, fmt.Errorf("definition %s/%s missing workflow labels (namespace=%q, name=%q, version=%q)", def.Namespace, def.Name, ns, name, version)
	}
	return routing.WorkflowKey{Namespace: ns, Name: name, Version: version}, nil
}
