package topology

import (
	"crypto/sha256"
	"fmt"
	"strings"

	v1 "github.com/kubesmarts/logic-operator/api/v1"
)

func (g *graph) infrastructure(ns string, runtimes []v1.LogicFlowRuntime, platforms []v1.ClusterLogicPlatform, platformsAvailable bool) {
	byName := map[string]*v1.ClusterLogicPlatform{}
	include := map[string]bool{}
	for i := range platforms {
		p := &platforms[i]
		byName[p.Name] = p
		include[p.Name] = ns == "" || p.Spec.TargetNamespace == ns
	}
	for _, rt := range runtimes {
		n := g.addNode(runtimeKind, rt.Namespace, rt.Name, phase(rt.Generation, rt.Status.ObservedGeneration, rt.Status.Phase))
		n.Details = map[string]any{"replicas": rt.Status.Replicas, "readyReplicas": rt.Status.ReadyReplicas}
		if !platformsAvailable {
			continue
		}
		var p *v1.ClusterLogicPlatform
		if rt.Spec.PlatformRef != nil && rt.Spec.PlatformRef.Name != "" {
			p = byName[rt.Spec.PlatformRef.Name]
		} else if len(platforms) == 1 {
			p = &platforms[0]
		}
		if p == nil {
			if !v1.IsEphemeral(&rt) || rt.Spec.PlatformRef != nil {
				g.warn("UnresolvedReference", runtimeKind, n.ID)
			}
			continue
		}
		include[p.Name] = true
		g.link(n.ID, "usesPlatform", id(platform, "", p.Name), nil)
		if !v1.IsEphemeral(&rt) && p.Spec.Runtime != nil && p.Spec.Runtime.Persistence != nil {
			persistence := p.Spec.Runtime.Persistence
			g.schema(n, &persistence.Runtime)
			g.schema(n, &persistence.Quartz)
		}
	}
	for _, p := range platforms {
		if include[p.Name] {
			g.addPlatform(&p)
		}
	}
}

func (g *graph) addPlatform(p *v1.ClusterLogicPlatform) {
	n := g.addNode(platform, "", p.Name, phase(p.Generation, p.Status.ObservedGeneration, p.Status.Phase))
	components := map[string]string{}
	n.Details = map[string]any{"targetNamespace": p.Spec.TargetNamespace, "components": components}
	component := func(kind, field string, configured bool, ready *bool, workload string) *node {
		if !configured {
			components[kind] = "NotConfigured"
			return nil
		}
		s := status{"Unknown", "status." + field + ".ready"}
		if ready != nil && p.Generation == p.Status.ObservedGeneration {
			s.State = readiness(*ready)
		}
		components[kind] = s.State
		// Component identity follows its owning platform across workload replacements.
		c := &node{ID: id(kind, "", p.Name), Kind: kind, Namespace: p.Spec.TargetNamespace, Name: p.Name, Label: kind, Status: s}
		if workload != "" {
			c.Name = workload
			c.Details = map[string]any{"workloadName": workload}
		}
		g.nodes[c.ID] = c
		g.link(n.ID, "manages", c.ID, nil)
		return c
	}
	var gateway, index, vector *bool
	var gatewayName, indexName, vectorName string
	if s := p.Status.Gateway; s != nil {
		gateway = &s.Ready
		gatewayName = s.DeploymentRef.Name
	}
	if s := p.Status.DataIndex; s != nil {
		index = &s.Ready
		indexName = s.Service.DeploymentRef.Name
	}
	if s := p.Status.Vector; s != nil {
		vector = &s.Ready
		vectorName = s.DaemonSetRef.Name
	}
	component("WorkflowGateway", "gateway", p.Spec.Gateway != nil, gateway, gatewayName)
	component("Vector", "vector", p.Spec.Vector != nil, vector, vectorName)
	if c := component("DataIndex", "dataIndex", p.Spec.DataIndex != nil, index, indexName); c != nil {
		g.schema(c, p.Spec.DataIndex.Persistence)
	}
}

func (g *graph) schema(consumer *node, store *v1.DatastoreSpec) {
	if store == nil || store.PostgreSQL == nil {
		return
	}
	ref := store.PostgreSQL.ServiceRef
	if ref.Name == "" || ref.DatabaseSchema == "" {
		return
	}
	port := 5432
	if ref.Port != nil {
		port = *ref.Port
	}
	host := ref.Name
	if ref.Namespace != "" {
		host = ref.Name + "." + ref.Namespace + ".svc.cluster.local"
	} else if !strings.Contains(host, ".") {
		host = host + "." + consumer.Namespace + ".svc.cluster.local"
	}
	// Hash the database identity so IDs distinguish targets without publishing addresses.
	database := ref.DatabaseName
	if database == "" {
		database = "logicflow"
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", host, port, database, ref.DatabaseSchema)))
	key := fmt.Sprintf("PostgreSQLSchema/%x", digest)
	g.nodes[key] = &node{ID: key, Kind: "PostgreSQLSchema", Name: ref.DatabaseSchema, Label: ref.DatabaseSchema, Status: status{"Unknown", "spec.persistence"}}
	g.link(consumer.ID, "usesSchema", key, nil)
}
