package inbox

import (
	"context"
	"testing"
)

func TestActorContext(t *testing.T) {
	ctx := context.Background()

	// Empty context returns ""
	if got := ActorFrom(ctx); got != "" {
		t.Errorf("ActorFrom(empty) = %q, want empty", got)
	}

	// Set and retrieve
	ctx = WithActor(ctx, "user:operator")
	if got := ActorFrom(ctx); got != "user:operator" {
		t.Errorf("ActorFrom = %q, want %q", got, "user:operator")
	}

	// Override
	ctx = WithActor(ctx, "user:admin")
	if got := ActorFrom(ctx); got != "user:admin" {
		t.Errorf("ActorFrom = %q, want %q", got, "user:admin")
	}
}
