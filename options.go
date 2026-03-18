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
