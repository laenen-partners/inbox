package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/laenen-partners/entitystore/store"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/tags"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
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
	events         []*inboxv1.Event
	newStatus      string
	transitionEvt  *inboxv1.Event // event for the status transition, written with the status
	newPayload     *payloadUpdate
	tagsAdd        []string
	tagsRemove     []string
	response       *Response
}

type payloadUpdate struct {
	payloadType string
	payload     json.RawMessage
}

// On starts a batch operation on an item. The actor is derived from
// the identity in context and used for all events emitted by this operation.
func (ib *Inbox) On(ctx context.Context, itemID string) *Op {
	actor := actorFromCtx(ctx)
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
		Action:  action,
		Comment: comment,
	}
	op.events = append(op.events, newProtoEventWithDetail(op.actor, action, &inboxv1.ItemResponded{
		Action:  action,
		Comment: comment,
	}))
	return op
}

// RespondWithData records a response with associated data (e.g. form field
// values). The data is persisted in the ItemResponded event's payload field
// as a structpb.Struct wrapped in an anypb.Any.
func (op *Op) RespondWithData(action string, comment string, data json.RawMessage) *Op {
	if op.err != nil {
		return op
	}
	op.response = &Response{
		Action:  action,
		Comment: comment,
		Data:    data,
	}
	evtData := &inboxv1.ItemResponded{
		Action:  action,
		Comment: comment,
	}
	if len(data) > 0 {
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			op.err = fmt.Errorf("inbox: RespondWithData: invalid JSON data: %w", err)
			return op
		}
		st, err := structpb.NewStruct(m)
		if err != nil {
			op.err = fmt.Errorf("inbox: RespondWithData: cannot convert data to struct: %w", err)
			return op
		}
		evtData.Payload, err = anypb.New(st)
		if err != nil {
			op.err = fmt.Errorf("inbox: RespondWithData: cannot pack data as Any: %w", err)
			return op
		}
	}
	op.events = append(op.events, newProtoEventWithDetail(op.actor, action, evtData))
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
		op.transitionEvt = newProtoEvent(op.actor, msg)
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

// needsTwoPhase reports whether this operation requires the two-phase
// apply path. Two-phase is needed when all of: there is a response,
// there is a status transition, a dispatcher is configured, and the
// item has a callback tag.
func (op *Op) needsTwoPhase() bool {
	return op.response != nil &&
		op.newStatus != "" &&
		op.ib.dispatcher != nil &&
		callbackValue(op.item.Tags) != ""
}

// Apply flushes all collected mutations and events to the entity store.
// When dispatch is required (response + status transition + dispatcher +
// callback tag), Apply uses a two-phase write so that a dispatch failure
// does not leave the item in a completed state. Otherwise, a single
// write is used.
func (op *Op) Apply() (Item, error) {
	if op.err != nil {
		return Item{}, op.err
	}
	if op.needsTwoPhase() {
		return op.applyTwoPhase()
	}
	return op.applySinglePhase()
}

// applySinglePhase is the original single-write path: apply all
// mutations, write once, fire dispatcher best-effort, fire hooks.
func (op *Op) applySinglePhase() (Item, error) {
	now := time.Now().UTC()
	item := op.item

	op.applyMutations(&item, now)

	if _, err := op.writeEntity(item); err != nil {
		return Item{}, fmt.Errorf("inbox: apply op: %w", err)
	}

	// Fire dispatcher best-effort (no two-phase guarantee).
	if op.response != nil && op.ib.dispatcher != nil {
		if cb := callbackValue(item.Tags); cb != "" {
			_ = op.ib.dispatcher.Dispatch(op.ctx, cb, item.ID, *op.response)
		}
	}

	return op.fireHooksIfNeeded(item)
}

// applyTwoPhase splits the entity store write into two phases so that
// dispatch failure does not leave the item in a terminal state.
//
//  1. Phase 1 — persist response events, payload, and tags, but NOT
//     the status transition. The item stays at its current status.
//  2. Dispatch — fire dispatcher.Dispatch(). If it fails, return error.
//     The response is already persisted but the item is not completed.
//  3. Phase 2 — persist the status transition. Fire lifecycle hooks.
func (op *Op) applyTwoPhase() (Item, error) {
	now := time.Now().UTC()
	item := op.item

	// Phase 1: apply everything except the status transition.
	// Temporarily clear newStatus so applyMutations skips the status
	// update, then restore it for phase 2.
	savedStatus := op.newStatus
	op.newStatus = ""
	op.applyMutations(&item, now)
	op.newStatus = savedStatus

	if _, err := op.writeEntity(item); err != nil {
		return Item{}, fmt.Errorf("inbox: apply op phase 1: %w", err)
	}

	// Dispatch — between the two writes.
	cb := callbackValue(item.Tags)
	if err := op.ib.dispatcher.Dispatch(op.ctx, cb, item.ID, *op.response); err != nil {
		return Item{}, fmt.Errorf("inbox: dispatch failed: %w", err)
	}

	// Phase 2: apply the status transition and its event.
	item.Tags = item.Tags.With("status", op.newStatus)
	item.Proto.Status = op.newStatus
	if op.transitionEvt != nil {
		if op.transitionEvt.GetAt() == nil {
			op.transitionEvt.At = timestamppb.New(now)
		}
		item.Proto.Events = append(item.Proto.Events, op.transitionEvt)
	}

	if _, err := op.writeEntity(item); err != nil {
		return Item{}, fmt.Errorf("inbox: apply op phase 2: %w", err)
	}

	return op.fireHooksIfNeeded(item)
}

// applyMutations applies payload, status, tags, and events to item in
// memory. Shared by both single-phase and two-phase paths. When
// op.newStatus is empty, the status mutation and its transition event
// are skipped — this is used by the two-phase path to defer the status
// change to phase 2.
func (op *Op) applyMutations(item *Item, now time.Time) {
	// Apply payload update.
	if op.newPayload != nil {
		item.Proto.PayloadType = op.newPayload.payloadType
		// For now, Op.UpdatePayload takes json.RawMessage — we store it
		// by clearing the proto Payload field. The payload type string is
		// still queryable.
		item.Proto.Payload = nil
	}

	// Apply status transition (without event — event appended last to
	// preserve builder call order).
	if op.newStatus != "" {
		item.Tags = item.Tags.With("status", op.newStatus)
		item.Proto.Status = op.newStatus
	}

	// Apply tag changes.
	for _, t := range op.tagsAdd {
		item.Tags = item.Tags.Merge(tags.MustNew(t))
	}
	for _, t := range op.tagsRemove {
		if k, _, ok := strings.Cut(t, ":"); ok {
			item.Tags = item.Tags.Without(k)
		}
	}

	// Stamp and append all collected events.
	for i := range op.events {
		if op.events[i].GetAt() == nil {
			op.events[i].At = timestamppb.New(now)
		}
	}
	item.Proto.Events = append(item.Proto.Events, op.events...)

	// Append the transition event last to match builder call order
	// (TransitionTo is typically the last call before Apply).
	if op.newStatus != "" && op.transitionEvt != nil {
		if op.transitionEvt.GetAt() == nil {
			op.transitionEvt.At = timestamppb.New(now)
		}
		item.Proto.Events = append(item.Proto.Events, op.transitionEvt)
	}
}

// writeEntity persists the item to the entity store.
func (op *Op) writeEntity(item Item) (Item, error) {
	_, err := op.ib.es.BatchWrite(op.ctx, []store.BatchWriteOp{
		{
			WriteEntity: &store.WriteEntityOp{
				Action:          store.WriteActionUpdate,
				MatchedEntityID: item.ID,
				Data:            item.Proto,
				Tags:            item.Tags.Strings(),
			},
		},
	})
	return item, err
}

// fireHooksIfNeeded fires lifecycle hooks if a status transition
// occurred. Hook errors are returned but do not roll back the
// transition (it is already persisted).
func (op *Op) fireHooksIfNeeded(item Item) (Item, error) {
	if op.newStatus == "" {
		return item, nil
	}
	var hookErr error
	switch op.newStatus {
	case StatusCompleted:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error { return h.OnComplete(op.ctx, item.ID, op.actor) })
	case StatusCancelled:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error { return h.OnCancel(op.ctx, item.ID, op.actor, "") })
	case StatusExpired:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error { return h.OnExpire(op.ctx, item.ID) })
	case StatusClaimed:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error { return h.OnClaim(op.ctx, item.ID, op.actor) })
	case StatusOpen:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error { return h.OnRelease(op.ctx, item.ID, op.actor) })
	}
	if hookErr != nil {
		return item, hookErr
	}
	return item, nil
}
