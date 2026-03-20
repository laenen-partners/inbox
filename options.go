package inbox

// Option configures an Inbox instance.
type Option func(*Inbox)

// WithDispatcher sets the callback dispatcher used to deliver responses
// to the item's creator (e.g. a workflow engine). When set, Respond
// fires the dispatcher after recording the response event.
func WithDispatcher(d Dispatcher) Option {
	return func(ib *Inbox) {
		ib.dispatcher = d
	}
}

// WithLifecycleHooks registers lifecycle hooks for items whose
// PayloadType matches payloadType. Hooks fire after successful
// state transitions (claim, release, complete, cancel, expire).
func WithLifecycleHooks(payloadType string, hooks LifecycleHooks) Option {
	return func(ib *Inbox) {
		if ib.hooks == nil {
			ib.hooks = make(map[string]LifecycleHooks)
		}
		ib.hooks[payloadType] = hooks
	}
}
