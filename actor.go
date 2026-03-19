package inbox

import "context"

type actorCtxKey struct{}

// WithActor stores an actor identifier in the context.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, actor)
}

// ActorFrom extracts the actor from the context. Returns "" if not set.
func ActorFrom(ctx context.Context) string {
	if v, ok := ctx.Value(actorCtxKey{}).(string); ok {
		return v
	}
	return ""
}
