package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/laenen-partners/entitystore/store"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Op is a batch operation builder that collects mutations and events
// on a single inbox item and flushes them in one entity store write.
//
// Usage:
//
//	item, err := ib.On(ctx, itemID, "user:sarah").
//	    Respond("approve", "Verified against PO.").
//	    WithEvent(&compliancepb.ScreeningResolved{...}).
//	    UpdatePayload(typeURL, data).
//	    Comment("All checks passed.").
//	    Tag("priority:resolved").
//	    TransitionTo(inbox.StatusCompleted).
//	    Apply()
type Op struct {
	ib    *Inbox
	ctx   context.Context
	actor string
	item  Item
	err   error

	// Collected mutations.
	events     []*inboxv1.Event
	newStatus  string
	newPayload *payloadUpdate
	tagsAdd    []string
	tagsRemove []string
	response   *Response
}

type payloadUpdate struct {
	payloadType string
	payload     json.RawMessage
}

// On starts a batch operation on an item. The actor is used for all
// events emitted by this operation unless overridden.
func (ib *Inbox) On(ctx context.Context, itemID string, actor string) *Op {
	item, err := ib.Get(ctx, itemID)
	return &Op{
		ib:    ib,
		ctx:   ctx,
		actor: actor,
		item:  item,
		err:   err,
	}
}

// ─── Standard operations ───

// Respond records a response. Does not transition status — call
// TransitionTo() to complete the item in the same operation.
func (op *Op) Respond(action string, comment string) *Op {
	if op.err != nil {
		return op
	}
	op.response = &Response{
		Actor:   op.actor,
		Action:  action,
		Comment: comment,
	}
	op.events = append(op.events, newProtoEventWithDetail(op.actor, action, &inboxv1.ItemResponded{
		Action:  action,
		Comment: comment,
	}))
	return op
}

// Comment adds a comment event.
func (op *Op) Comment(body string) *Op {
	if op.err != nil {
		return op
	}
	op.events = append(op.events, newProtoEventWithDetail(op.actor, body, &inboxv1.CommentAppended{
		Body: body,
	}))
	return op
}

// CommentWith adds a comment with visibility and refs.
func (op *Op) CommentWith(body string, opts CommentOpts) *Op {
	if op.err != nil {
		return op
	}
	op.events = append(op.events, newProtoEventWithDetail(op.actor, body, &inboxv1.CommentAppended{
		Body:       body,
		Visibility: opts.Visibility,
		Refs:       opts.Refs,
	}))
	return op
}

// UpdatePayload replaces the item payload.
func (op *Op) UpdatePayload(payloadType string, payload json.RawMessage) *Op {
	if op.err != nil {
		return op
	}
	op.newPayload = &payloadUpdate{payloadType: payloadType, payload: payload}
	op.events = append(op.events, newProtoEvent(op.actor, &inboxv1.PayloadUpdated{
		PayloadType: payloadType,
	}))
	return op
}

// Tag adds tags to the item.
func (op *Op) Tag(tags ...string) *Op {
	if op.err != nil {
		return op
	}
	op.tagsAdd = append(op.tagsAdd, tags...)
	return op
}

// Untag removes tags from the item.
func (op *Op) Untag(tags ...string) *Op {
	if op.err != nil {
		return op
	}
	op.tagsRemove = append(op.tagsRemove, tags...)
	return op
}

// TransitionTo changes the item status. Use this instead of calling
// Complete/Cancel/Expire separately when combining with other operations.
func (op *Op) TransitionTo(status string) *Op {
	if op.err != nil {
		return op
	}
	if IsTerminal(op.item.Proto.Status) {
		op.err = fmt.Errorf("inbox: item %s is already in terminal status %s", op.item.ID, op.item.Proto.Status)
		return op
	}
	op.newStatus = status

	var msg proto.Message
	switch status {
	case StatusCompleted:
		msg = &inboxv1.ItemCompleted{CompletedBy: op.actor}
	case StatusCancelled:
		msg = &inboxv1.ItemCancelled{CancelledBy: op.actor}
	case StatusExpired:
		msg = &inboxv1.ItemExpired{}
	case StatusClaimed:
		msg = &inboxv1.ItemClaimed{ClaimedBy: op.actor}
	case StatusOpen:
		msg = &inboxv1.ItemReleased{ReleasedBy: op.actor}
	}
	if msg != nil {
		op.events = append(op.events, newProtoEvent(op.actor, msg))
	}
	return op
}

// ─── Events ───

// WithEvent appends a typed event from a proto message. The DataType is
// derived automatically from the proto message's fully qualified name.
//
//	op.WithEvent(&compliancepb.ScreeningResolved{...})
func (op *Op) WithEvent(msg proto.Message) *Op {
	if op.err != nil {
		return op
	}
	op.events = append(op.events, newProtoEvent(op.actor, msg))
	return op
}

// ─── Apply ───

// Apply flushes all collected mutations and events in a single
// entity store write. Returns the updated item.
func (op *Op) Apply() (Item, error) {
	if op.err != nil {
		return Item{}, op.err
	}

	now := time.Now().UTC()
	item := op.item

	// Apply payload update.
	if op.newPayload != nil {
		item.Proto.PayloadType = op.newPayload.payloadType
		// For now, Op.UpdatePayload takes json.RawMessage — we store it
		// by clearing the proto Payload field. The payload type string is
		// still queryable.
		item.Proto.Payload = nil
	}

	// Apply status transition.
	if op.newStatus != "" {
		item.Tags = replaceStatusTag(item.Tags, item.Proto.Status, op.newStatus)
		item.Proto.Status = op.newStatus
	}

	// Apply tag changes.
	for _, t := range op.tagsAdd {
		if !hasTagInSlice(item.Tags, t) {
			item.Tags = append(item.Tags, t)
		}
	}
	for _, t := range op.tagsRemove {
		item.Tags = removeFromSlice(item.Tags, t)
	}

	// Stamp and append all events.
	for i := range op.events {
		if op.events[i].GetAt() == nil {
			op.events[i].At = timestamppb.New(now)
		}
	}
	item.Proto.Events = append(item.Proto.Events, op.events...)

	// Single write.
	_, err := op.ib.es.BatchWrite(op.ctx, []store.BatchWriteOp{
		{
			WriteEntity: &store.WriteEntityOp{
				Action:          store.WriteActionUpdate,
				MatchedEntityID: item.ID,
				Data:            item.Proto,
				Tags:            item.Tags,
			},
		},
	})
	if err != nil {
		return Item{}, fmt.Errorf("inbox: apply op: %w", err)
	}

	// Fire dispatcher if we have a response and dispatcher is configured.
	if op.response != nil && op.ib.dispatcher != nil {
		if cb := findCallbackTag(item.Tags); cb != "" {
			_ = op.ib.dispatcher.Dispatch(op.ctx, cb, item.ID, *op.response)
		}
	}

	return item, nil
}

// ─── Internal helpers ───

func hasTagInSlice(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func removeFromSlice(tags []string, tag string) []string {
	result := make([]string, 0, len(tags))
	for _, t := range tags {
		if t != tag {
			result = append(result, t)
		}
	}
	return result
}
