package topology

import (
	"sort"

	v1 "github.com/kubesmarts/logic-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	definition  = "LogicFlowDefinition"
	service     = "LogicFlowService"
	runtimeKind = "LogicFlowRuntime"
	platform    = "ClusterLogicPlatform"
)

type status struct {
	State  string `json:"state"`
	Source string `json:"source"`
}

type node struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Namespace string         `json:"namespace,omitempty"`
	Name      string         `json:"name"`
	Label     string         `json:"label"`
	Status    status         `json:"status"`
	Details   map[string]any `json:"details,omitempty"`
}

type edge struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
	Weight *int32 `json:"weight,omitempty"`
}

type warning struct {
	Code   string `json:"code"`
	Kind   string `json:"kind"`
	NodeID string `json:"nodeId,omitempty"`
}

type discovery struct {
	Partial  bool      `json:"partial"`
	Warnings []warning `json:"warnings"`
}

type response struct {
	Nodes     []*node   `json:"nodes"`
	Edges     []edge    `json:"edges"`
	Discovery discovery `json:"discovery"`
}

type graph struct {
	nodes    map[string]*node
	edges    map[string]edge
	warnings map[warning]bool
}

func newGraph() *graph {
	return &graph{nodes: map[string]*node{}, edges: map[string]edge{}, warnings: map[warning]bool{}}
}

func (g *graph) warn(code, kind, id string) { g.warnings[warning{code, kind, id}] = true }

func id(kind, ns, name string) string { return kind + "/" + ns + "/" + name }

func (g *graph) addNode(kind, ns, name string, s status) *node {
	n := &node{ID: id(kind, ns, name), Kind: kind, Namespace: ns, Name: name, Label: name, Status: s}
	g.nodes[n.ID] = n
	return n
}

func (g *graph) link(source, rel, target string, weight *int32) {
	key := rel + ":" + source + ":" + target
	g.edges[key] = edge{key, rel, source, target, weight}
}

func (g *graph) reference(n *node, rel, kind, name string, weight *int32, available map[string]bool) {
	if available[kind] {
		g.link(n.ID, rel, id(kind, n.Namespace, name), weight)
	}
}

func (g *graph) result() response {
	out := response{Nodes: []*node{}, Edges: []edge{}, Discovery: discovery{Warnings: []warning{}}}
	for _, n := range g.nodes {
		out.Nodes = append(out.Nodes, n)
	}
	for _, e := range g.edges {
		if g.nodes[e.Target] == nil {
			g.warn("UnresolvedReference", g.nodes[e.Source].Kind, e.Source)
			continue
		}
		out.Edges = append(out.Edges, e)
	}
	for w := range g.warnings {
		out.Discovery.Warnings = append(out.Discovery.Warnings, w)
	}
	out.Discovery.Partial = len(out.Discovery.Warnings) > 0
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool { return out.Edges[i].ID < out.Edges[j].ID })
	sort.Slice(out.Discovery.Warnings, func(i, j int) bool {
		a, b := out.Discovery.Warnings[i], out.Discovery.Warnings[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.NodeID < b.NodeID
	})
	return out
}

func readiness(ok bool) string {
	if ok {
		return "Ready"
	}
	return "NotReady"
}

func condition(gen int64, conditions []metav1.Condition, typ string) status {
	s := status{"Unknown", "status.conditions." + typ}
	c := v1.FindCondition(conditions, typ)
	if c != nil && c.ObservedGeneration == gen && (c.Status == metav1.ConditionFalse || c.Status == metav1.ConditionTrue) {
		s.State = readiness(c.Status == metav1.ConditionTrue)
	}
	return s
}

func phase(gen, observed int64, p v1.ApplicationPhase) status {
	s := status{"Unknown", "status.phase"}
	if observed == gen {
		switch p {
		case v1.ApplicationPhaseReady:
			s.State = "Ready"
		case v1.ApplicationPhasePending, v1.ApplicationPhaseFailed:
			s.State = "NotReady"
		}
	}
	return s
}
