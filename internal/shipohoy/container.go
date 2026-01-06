package shipohoy

// GenericContainer is a minimal Container implementation.
type GenericContainer struct {
	SpecValue ContainerSpec
}

// Name returns the container name.
func (c *GenericContainer) Name() string { return c.SpecValue.Name }

// Spec returns the container spec.
func (c *GenericContainer) Spec() ContainerSpec { return c.SpecValue }
