package discovery

import (
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

// definitionResolver fetches a LogicFlowDefinition by name within a namespace.
type definitionResolver func(namespace, name string) (*logicv1.LogicFlowDefinition, error)

// buildRoutes constructs the routing table by following each Service's forward
// references to the Definitions it exposes. The routing key (namespace, name,
// version) is read from the Definition's labels; the URL comes from the Service
// status. Returns the map and a slice of non-fatal errors.
func buildRoutes(
	services []logicv1.LogicFlowService,
	resolve definitionResolver,
) (map[WorkflowKey]string, []error) {
	routes := make(map[WorkflowKey]string)
	var errs []error

	for i := range services {
		svc := &services[i]

		// A Service with no URL yet is simply not reconciled; skip it quietly
		// rather than logging an error on every recompute.
		if svc.Status.URL == "" {
			continue
		}

		if err := ValidateExternalURL(svc.Status.URL); err != nil {
			errs = append(errs, fmt.Errorf(
				"service %s/%s has invalid URL %q: %w",
				svc.Namespace, svc.Name, svc.Status.URL, err,
			))
			continue
		}

		for _, name := range referencedDefinitions(svc) {
			def, err := resolve(svc.Namespace, name)
			if err != nil {
				errs = append(errs, fmt.Errorf(
					"service %s/%s references definition %q: %w",
					svc.Namespace, svc.Name, name, err,
				))
				continue
			}

			key, err := workflowKeyFromLabels(def)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			routes[key] = svc.Status.URL
		}
	}

	return routes, errs
}

// referencedDefinitions returns the definition names a Service exposes, from
// either DefaultDefinition (100% traffic) or the Traffic split.
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

// workflowKeyFromLabels extracts the routing key from a Definition's labels.
func workflowKeyFromLabels(def *logicv1.LogicFlowDefinition) (WorkflowKey, error) {
	ns := def.Labels[logicv1.LabelWorkflowNamespace]
	name := def.Labels[logicv1.LabelWorkflowName]
	version := def.Labels[logicv1.LabelWorkflowVersion]

	if ns == "" || name == "" || version == "" {
		return WorkflowKey{}, fmt.Errorf(
			"definition %s/%s missing workflow labels (namespace=%q, name=%q, version=%q)",
			def.Namespace, def.Name, ns, name, version,
		)
	}

	return WorkflowKey{Namespace: ns, Name: name, Version: version}, nil
}
