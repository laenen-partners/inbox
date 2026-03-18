package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/laenen-partners/entitystore/store"
	"google.golang.org/protobuf/proto"
)

// Op is a batch operation builder that collects mutations and events
// on a single inbox item and flushes them in one entity store write.
//
// Usage:
//
//	item, err := ib.On(ctx, itemID, "user:sarah").
//	    Respond("approve", "Verified against PO.").
//	    UpdatePayload(typeURL, data).
//	    Tag("priority:resolved").
//	    Emit("acme.v1.CreditCheckCompleted", creditCheckResult).
//	    Comment("All checks passed.").
//	    Apply()
type Op struct {
	ib    *Inbox
	ctx   context.Context
	actor string
	item  Item
	err   error

	// Collected mutations.
	events     []Event
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
// Complete() separately or use TransitionTo().
func (op *Op) Respond(action string, comment string) *Op {
	if op.err != nil {
		return op
	}
	op.response = &Response{
		Actor:   op.actor,
		Action:  action,
		Comment: comment,
	}
	op.events = append(op.events, newTypedEvent(op.actor, ActionResponded, action, TypeItemResponded, ItemResponded{
		Action:  action,
		Comment: comment,
	}))
	return op
}

// RespondWith records a response with custom typed data.
func (op *Op) RespondWith(action string, comment string, dataType string, data any) *Op {
	if op.err != nil {
		return op
	}
	raw, _ := json.Marshal(data)
	op.response = &Response{
		Actor:   op.actor,
		Action:  action,
		Comment: comment,
		Data:    raw,
	}
	op.events = append(op.events, Event{
		Actor:    op.actor,
		Action:   ActionResponded,
		Detail:   action,
		DataType: dataType,
		Data:     raw,
	})
	return op
}

// Comment adds a comment event.
func (op *Op) Comment(body string) *Op {
	if op.err != nil {
		return op
	}
	op.events = append(op.events, newTypedEvent(op.actor, ActionCommented, body, TypeCommentAppended, CommentAppended{
		Body: body,
	}))
	return op
}

// CommentWith adds a comment with visibility and refs.
func (op *Op) CommentWith(body string, opts CommentOpts) *Op {
	if op.err != nil {
		return op
	}
	op.events = append(op.events, newTypedEvent(op.actor, ActionCommented, body, TypeCommentAppended, CommentAppended{
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
	op.events = append(op.events, newTypedEvent(op.actor, "payload_updated", "", TypePayloadUpdated, PayloadUpdated{
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
	if IsTerminal(op.item.Status) {
		op.err = fmt.Errorf("inbox: item %s is already in terminal status %s", op.item.ID, op.item.Status)
		return op
	}
	op.newStatus = status

	var dataType string
	var data any
	switch status {
	case StatusCompleted:
		dataType = TypeItemCompleted
		data = ItemCompleted{CompletedBy: op.actor}
	case StatusCancelled:
		dataType = TypeItemCancelled
		data = ItemCancelled{CancelledBy: op.actor}
	case StatusExpired:
		dataType = TypeItemExpired
		data = ItemExpired{}
	case StatusClaimed:
		dataType = TypeItemClaimed
		data = ItemClaimed{ClaimedBy: op.actor}
	case StatusOpen:
		dataType = TypeItemReleased
		data = ItemReleased{ReleasedBy: op.actor}
	}
	if dataType != "" {
		op.events = append(op.events, newTypedEvent(op.actor, status, "", dataType, data))
	}
	return op
}

// ─── Custom events ───

// WithEvent appends a typed event from a proto message. The data_type
// is derived automatically from the proto message's fully qualified
// name, and the message is marshaled to JSON via protobuf's Any
// encoding. This is the primary way to emit custom domain events.
//
//	op.WithEvent("screening_resolved", &compliancepb.ScreeningResolved{...})
func (op *Op) WithEvent(action string, msg proto.Message) *Op {
	if op.err != nil {
		return op
	}
	typeURL, raw, err := PackEventData(msg)
	if err != nil {
		op.err = fmt.Errorf("inbox: marshal event proto: %w", err)
		return op
	}
	op.events = append(op.events, Event{
		Actor:    op.actor,
		Action:   action,
		DataType: typeURL,
		Data:     raw,
	})
	return op
}

// Emit appends a custom typed event with a plain Go struct.
// The dataType is your fully qualified type name and data is
// any JSON-serializable struct.
func (op *Op) Emit(action string, dataType string, data any) *Op {
	if op.err != nil {
		return op
	}
	raw, err := json.Marshal(data)
	if err != nil {
		op.err = fmt.Errorf("inbox: marshal event data: %w", err)
		return op
	}
	op.events = append(op.events, Event{
		Actor:    op.actor,
		Action:   action,
		DataType: dataType,
		Data:     raw,
	})
	return op
}

// EmitRaw appends a custom event with pre-serialized JSON data.
func (op *Op) EmitRaw(action string, dataType string, data json.RawMessage) *Op {
	if op.err != nil {
		return op
	}
	op.events = append(op.events, Event{
		Actor:    op.actor,
		Action:   action,
		DataType: dataType,
		Data:     data,
	})
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
		item.PayloadType = op.newPayload.payloadType
		item.Payload = op.newPayload.payload
	}

	// Apply status transition.
	if op.newStatus != "" {
		item.Tags = replaceStatusTag(item.Tags, item.Status, op.newStatus)
		item.Status = op.newStatus
	}

	// Apply tag changes to the local copy for the entity store write.
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
		if op.events[i].At.IsZero() {
			op.events[i].At = now
		}
	}
	item.Events = append(item.Events, op.events...)

	// Single write.
	data, err := marshalItemData(item)
	if err != nil {
		return Item{}, fmt.Errorf("inbox: marshal item: %w", err)
	}

	_, err = op.ib.es.BatchWrite(op.ctx, []store.BatchWriteOp{
		{
			WriteEntity: &store.WriteEntityOp{
				Action:          store.WriteActionUpdate,
				EntityType:      EntityType,
				MatchedEntityID: item.ID,
				Data:            data,
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
