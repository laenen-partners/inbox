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
package inbox

import (
	"github.com/laenen-partners/entitystore"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
)

// EntityType is the entity store type for all inbox items.
// Derived from the proto-generated match config.
var EntityType = inboxv1.ItemMatchConfig().EntityType

// Inbox provides operations on inbox items backed by an entity store.
type Inbox struct {
	es *entitystore.EntityStore
}

// New creates an Inbox backed by the given entity store.
func New(es *entitystore.EntityStore) *Inbox {
	return &Inbox{es: es}
}
