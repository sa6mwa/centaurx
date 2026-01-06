package sessionprefs

import (
	"context"
	"testing"
)

func TestWithContextAndFromContext(t *testing.T) {
	prefs := New()
	prefs.FullCommandOutput = true

	ctx := WithContext(context.Background(), prefs)
	got := FromContext(ctx)
	if got == nil {
		t.Fatalf("expected prefs")
	}
	if !got.FullCommandOutput {
		t.Fatalf("expected pref to be preserved")
	}
}

func TestWithContextNil(t *testing.T) {
	var nilCtx context.Context
	ctx := WithContext(nilCtx, New())
	if ctx != nil {
		t.Fatalf("expected nil context")
	}
	ctx = WithContext(context.Background(), nil)
	if ctx == nil {
		t.Fatalf("expected non-nil context to pass through")
	}
	if FromContext(context.Background()) != nil {
		t.Fatalf("expected no prefs for empty context")
	}
}
