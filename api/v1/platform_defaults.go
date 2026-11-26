package v1

import (
	"encoding/json"
)

// ToApplicationSpec converts the defaults into an ApplicationSpec via JSON, since both types share JSON paths.
// Probe tunings are excluded: a timing-only probe is not a valid corev1.Probe. Use ProbeDefaults for them.
func (d *ApplicationDefaultsSpec) ToApplicationSpec() (ApplicationSpec, error) {
	var app ApplicationSpec
	if d == nil {
		return app, nil
	}

	c := d.DeepCopy()
	c.Container.LivenessProbe = nil
	c.Container.ReadinessProbe = nil
	c.Container.StartupProbe = nil
	c.PodTemplate.Container.LivenessProbe = nil
	c.PodTemplate.Container.ReadinessProbe = nil
	c.PodTemplate.Container.StartupProbe = nil

	data, err := json.Marshal(c)
	if err != nil {
		return app, err
	}
	if err := json.Unmarshal(data, &app); err != nil {
		return app, err
	}
	return app, nil
}

// ProbeDefaults returns the probe tunings, flattened so that podTemplate.container wins over container.
// Each probe is replaced whole, matching the one-level merge depth of the container tiers.
func (d *ApplicationDefaultsSpec) ProbeDefaults() (live, ready, startup *ProbeTuning) {
	if d == nil {
		return nil, nil, nil
	}
	for _, c := range []*ContainerDefaultsSpec{&d.Container, &d.PodTemplate.Container} {
		if c.LivenessProbe != nil {
			live = c.LivenessProbe.DeepCopy()
		}
		if c.ReadinessProbe != nil {
			ready = c.ReadinessProbe.DeepCopy()
		}
		if c.StartupProbe != nil {
			startup = c.StartupProbe.DeepCopy()
		}
	}
	return live, ready, startup
}
