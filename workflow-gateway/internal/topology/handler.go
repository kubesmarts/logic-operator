// Package topology projects operator resources into a graph for the operational console.
package topology

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	v1 "github.com/kubesmarts/logic-operator/api/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Handler reads each resource kind independently so unavailable sources do not
// hide resources discovered successfully. The reader uses the gateway identity.
func Handler(reader client.Reader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		query, err := url.ParseQuery(r.URL.RawQuery)
		ns := query.Get("namespace")
		if err != nil || (query.Has("namespace") && (len(query["namespace"]) != 1 || len(validation.IsDNS1123Label(ns)) != 0)) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid namespace filter"})
			return
		}
		g := newGraph()
		var defs v1.LogicFlowDefinitionList
		var services v1.LogicFlowServiceList
		var runtimes v1.LogicFlowRuntimeList
		var platforms v1.ClusterLogicPlatformList
		sources := []struct {
			kind string
			list client.ObjectList
		}{
			{definition, &defs}, {service, &services}, {runtimeKind, &runtimes}, {platform, &platforms},
		}
		// A slow API read must leave time for the remaining sources and response.
		available := map[string]bool{}
		for _, src := range sources {
			var opts []client.ListOption
			if ns != "" && src.kind != platform {
				opts = append(opts, client.InNamespace(ns))
			}
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			ok := reader != nil && reader.List(ctx, src.list, opts...) == nil
			cancel()
			if !ok {
				g.warn("SourceUnavailable", src.kind, "")
				continue
			}
			available[src.kind] = true
		}
		if available[definition] {
			for _, d := range defs.Items {
				n := g.addNode(definition, d.Namespace, d.Name, condition(d.Generation, d.Status.Conditions, v1.ConditionReady))
				if d.Status.WorkflowName != "" {
					n.Label = d.Status.WorkflowName
				}
				if d.Status.WorkflowVersion != "" {
					n.Details = map[string]any{"version": d.Status.WorkflowVersion}
				}
				g.reference(n, "runsOn", runtimeKind, d.Spec.RuntimeRef.Name, nil, available)
			}
		}
		if available[service] {
			for _, s := range services.Items {
				n := g.addNode(service, s.Namespace, s.Name, condition(s.Generation, s.Status.Conditions, v1.ConditionIngressReady))
				if ref := s.Spec.DefaultDefinition; ref != nil {
					weight := int32(100)
					g.reference(n, "routesTo", definition, ref.Name, &weight, available)
				}
				for _, t := range s.Spec.Traffic {
					g.reference(n, "routesTo", definition, t.DefinitionRef.Name, &t.Weight, available)
				}
			}
		}
		if !available[platform] {
			platforms.Items = nil
		}
		if !available[runtimeKind] {
			runtimes.Items = nil
		}
		g.infrastructure(ns, runtimes.Items, platforms.Items, available[platform])
		result := g.result()
		if len(available) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(result)
	}
}
