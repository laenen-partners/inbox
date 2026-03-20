package inbox

import "context"

// LifecycleHooks lets producers react when the inbox transitions an
// item's state. Hooks are registered per payload type via
// WithLifecycleHooks and fire after the entity store write succeeds.
//
// Hook errors are returned to the caller but do NOT roll back the
// transition — the state change is already persisted.
type LifecycleHooks interface {
	OnClaim(ctx context.Context, itemID, actor string) error
	OnRelease(ctx context.Context, itemID, actor string) error
	OnCancel(ctx context.Context, itemID, actor, reason string) error
	OnComplete(ctx context.Context, itemID, actor string) error
	OnExpire(ctx context.Context, itemID string) error
}

// DefaultHooks is a no-op implementation of LifecycleHooks. Embed it
// in your struct to only override the hooks you care about.
type DefaultHooks struct{}

func (DefaultHooks) OnClaim(context.Context, string, string) error          { return nil }
func (DefaultHooks) OnRelease(context.Context, string, string) error        { return nil }
func (DefaultHooks) OnCancel(context.Context, string, string, string) error { return nil }
func (DefaultHooks) OnComplete(context.Context, string, string) error       { return nil }
func (DefaultHooks) OnExpire(context.Context, string) error                 { return nil }

// fireHook looks up hooks by payload type and calls fn.
func (ib *Inbox) fireHook(item Item, fn func(LifecycleHooks) error) error {
	if ib.hooks == nil {
		return nil
	}
	h, ok := ib.hooks[item.PayloadType()]
	if !ok {
		return nil
	}
	return fn(h)
}
