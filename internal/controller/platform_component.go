package controller

import (
	"context"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

// PlatformComponent defines the interface for a platform service component (Data Index, Vector, Workflow Gateway).
type PlatformComponent interface {
	// Name returns the component name for logging and error messages.
	Name() string

	// Reconcile drives the component's resources toward the desired state.
	Reconcile(ctx context.Context, plat *logicv1.LogicPlatform) error

	// UpdateStatus mutates plat.Status in-memory to reflect the component's observed state.
	// It must NOT call the API — the orchestrator writes .status once after all components.
	UpdateStatus(ctx context.Context, plat *logicv1.LogicPlatform) error
}

// componentFactory is a function that builds the ordered set of components for one reconcile.
type componentFactory func(r *LogicPlatformReconciler) []PlatformComponent

// defaultComponents returns the standard set of platform components (Data Index, Vector, Workflow Gateway).
func defaultComponents(r *LogicPlatformReconciler) []PlatformComponent {
	return []PlatformComponent{
		&dataIndexComponent{r},
		&vectorComponent{r},
		&workflowGatewayComponent{r},
	}
}

// platformComponents returns the platformComponents to reconcile, using the injected factory or the default.
func (r *LogicPlatformReconciler) components() []PlatformComponent {
	if r.platformComponents != nil {
		return r.platformComponents(r)
	}
	return defaultComponents(r)
}
