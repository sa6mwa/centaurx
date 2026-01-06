package sessionprefs

import (
	"context"

	"pkt.systems/centaurx/schema"
)

// Prefs captures per-session preferences.
type Prefs struct {
	FullCommandOutput bool
	ActiveTab         schema.TabID
}

type prefsKey struct{}

// New returns a new Prefs instance with defaults applied.
func New() *Prefs {
	return &Prefs{}
}

// WithContext stores prefs in the context.
func WithContext(ctx context.Context, prefs *Prefs) context.Context {
	if ctx == nil || prefs == nil {
		return ctx
	}
	return context.WithValue(ctx, prefsKey{}, prefs)
}

// FromContext returns the prefs stored in the context, if any.
func FromContext(ctx context.Context) *Prefs {
	if ctx == nil {
		return nil
	}
	if value := ctx.Value(prefsKey{}); value != nil {
		if prefs, ok := value.(*Prefs); ok {
			return prefs
		}
	}
	return nil
}
