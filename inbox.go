// Package inbox provides a task-driven inbox system backed by the entity store.
//
// Inbox items are units of work to be resolved by a human or AI agent.
// They are created by external systems (workflows, classifiers, scheduled
// jobs) and carry typed proto payloads, free-form tags, and an append-only
// event log.
//
// The inbox is payload-agnostic — it stores and delivers proto payloads
// without interpretation. Routing and filtering is done entirely via tags.
//
// Create an Inbox:
//
//	es, _ := entitystore.New(entitystore.WithPgStore(pool))
//	ib := inbox.New(es)
//
// With a callback dispatcher:
//
//	ib := inbox.New(es, inbox.WithDispatcher(myDispatcher))
package inbox

import (
	"github.com/laenen-partners/entitystore"
)

// EntityType is the entity store type for all inbox items.
const EntityType = "inbox.item"

// Inbox provides operations on inbox items backed by an entity store.
type Inbox struct {
	es         *entitystore.EntityStore
	dispatcher Dispatcher
}

// New creates an Inbox backed by the given entity store.
func New(es *entitystore.EntityStore, opts ...Option) *Inbox {
	ib := &Inbox{es: es}
	for _, opt := range opts {
		opt(ib)
	}
	return ib
}
