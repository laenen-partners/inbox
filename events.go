package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/laenen-partners/entitystore/store"
)

// Well-known event actions (past tense — events are facts).
const (
	ActionCreated    = "created"
	ActionClaimed    = "claimed"
	ActionReleased   = "released"
	ActionCommented  = "commented"
	ActionTagged     = "tagged"
	ActionResponded  = "responded"
	ActionEscalated  = "escalated"
	ActionExpired    = "expired"
	ActionCancelled  = "cancelled"
	ActionReassigned = "reassigned"
	ActionCompleted  = "completed"
)

// AddEvent appends an event to an item's event log.
// This is the general-purpose way to add context to an item without
// changing its status. Use the convenience methods (Comment, Escalate,
// Reassign) for common event types with typed data.
func (ib *Inbox) AddEvent(ctx context.Context, itemID string, evt Event) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}

	if evt.At.IsZero() {
		evt.At = time.Now().UTC()
	}
	item.Events = append(item.Events, evt)

	data, err := marshalItemData(item)
	if err != nil {
		return Item{}, fmt.Errorf("inbox: marshal item: %w", err)
	}

	_, err = ib.es.BatchWrite(ctx, []store.BatchWriteOp{
		{
			WriteEntity: &store.WriteEntityOp{
				Action:          store.WriteActionUpdate,
				EntityType:      EntityType,
				MatchedEntityID: itemID,
				Data:            data,
			},
		},
	})
	if err != nil {
		return Item{}, fmt.Errorf("inbox: add event: %w", err)
	}

	return item, nil
}

// ─── Standard event convenience methods ───

// CommentOpts configures a comment event.
type CommentOpts struct {
	// Visibility restricts who can see the comment.
	// Empty means visible to all.
	// Example: []string{"team:compliance"}
	Visibility []string

	// Refs are references to related entities.
	// Example: []string{"ref:document:DOC-123"}
	Refs []string
}

// Comment adds a comment to an item. Comments are the primary way
// humans and agents add context without changing item state.
func (ib *Inbox) Comment(ctx context.Context, itemID string, actor string, body string, opts *CommentOpts) (Item, error) {
	evtData := CommentAppended{Body: body}
	if opts != nil {
		evtData.Visibility = opts.Visibility
		evtData.Refs = opts.Refs
	}

	return ib.AddEvent(ctx, itemID, newTypedEvent(actor, ActionCommented, body, TypeCommentAppended, evtData))
}

// Escalate moves an item from one team to another with a reason.
// Updates team tags and records an ItemEscalated event.
func (ib *Inbox) Escalate(ctx context.Context, itemID string, actor string, fromTeam string, toTeam string, reason string) (Item, error) {
	if fromTeam != "" {
		_ = ib.es.RemoveTag(ctx, itemID, "team:"+fromTeam)
	}
	if toTeam != "" {
		_ = ib.es.AddTags(ctx, itemID, []string{"team:" + toTeam})
	}

	return ib.AddEvent(ctx, itemID, newTypedEvent(actor, ActionEscalated, reason, TypeItemEscalated, ItemEscalated{
		FromTeam: fromTeam,
		ToTeam:   toTeam,
		Reason:   reason,
	}))
}

// Reassign moves an item from one actor to another.
// Updates assignee tags and records an ItemReassigned event.
func (ib *Inbox) Reassign(ctx context.Context, itemID string, actor string, fromActor string, toActor string, reason string) (Item, error) {
	if fromActor != "" {
		_ = ib.es.RemoveTag(ctx, itemID, "assignee:"+fromActor)
	}
	if toActor != "" {
		_ = ib.es.AddTags(ctx, itemID, []string{"assignee:" + toActor})
	}

	return ib.AddEvent(ctx, itemID, newTypedEvent(actor, ActionReassigned, reason, TypeItemReassigned, ItemReassigned{
		FromActor: fromActor,
		ToActor:   toActor,
		Reason:    reason,
	}))
}

// ─── Internal ───

// newTypedEvent creates an event with typed data.
func newTypedEvent(actor, action, detail, dataType string, data any) Event {
	raw, _ := json.Marshal(data)
	return Event{
		Actor:    actor,
		Action:   action,
		Detail:   detail,
		DataType: dataType,
		Data:     raw,
	}
}
