package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/entitystore/store"
)

// Claim marks an item as claimed by the given actor. Only items with
// status "open" can be claimed. Returns the updated item.
func (ib *Inbox) Claim(ctx context.Context, itemID string, actor string) (Item, error) {
	return ib.transition(ctx, itemID, StatusOpen, StatusClaimed,
		newTypedEvent(actor, &inboxv1.ItemClaimed{ClaimedBy: actor}))
}

// Release returns a claimed item to "open" status.
func (ib *Inbox) Release(ctx context.Context, itemID string, actor string) (Item, error) {
	return ib.transition(ctx, itemID, StatusClaimed, StatusOpen,
		newTypedEvent(actor, &inboxv1.ItemReleased{ReleasedBy: actor}))
}

// Respond records a response on an item. This does NOT automatically
// transition the item to "completed" — the workflow owns lifecycle
// transitions via Complete(). Fires the dispatcher callback if configured.
func (ib *Inbox) Respond(ctx context.Context, itemID string, resp Response) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if IsTerminal(item.Status) {
		return Item{}, fmt.Errorf("inbox: item %s is in terminal status %s", itemID, item.Status)
	}

	evtData := &inboxv1.ItemResponded{
		Action:  resp.Action,
		Comment: resp.Comment,
	}
	evt := newTypedEventWithDetail(resp.Actor, resp.Action, evtData)
	if len(resp.Data) > 0 {
		evt.Data = resp.Data
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
		return Item{}, fmt.Errorf("inbox: respond to item: %w", err)
	}

	// Fire callback if dispatcher is configured.
	if ib.dispatcher != nil {
		if cb := findCallbackTag(item.Tags); cb != "" {
			_ = ib.dispatcher.Dispatch(ctx, cb, itemID, resp)
		}
	}

	return item, nil
}

// Complete transitions an item to "completed" status. Typically called
// by the workflow after it has processed the response.
func (ib *Inbox) Complete(ctx context.Context, itemID string, actor string) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if IsTerminal(item.Status) {
		return Item{}, fmt.Errorf("inbox: item %s is already in terminal status %s", itemID, item.Status)
	}
	return ib.doTransition(ctx, item, StatusCompleted,
		newTypedEvent(actor, &inboxv1.ItemCompleted{CompletedBy: actor}))
}

// Cancel marks an item as cancelled.
func (ib *Inbox) Cancel(ctx context.Context, itemID string, actor string, reason string) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if IsTerminal(item.Status) {
		return Item{}, fmt.Errorf("inbox: item %s is already in terminal status %s", itemID, item.Status)
	}
	return ib.doTransition(ctx, item, StatusCancelled,
		newTypedEventWithDetail(actor, reason, &inboxv1.ItemCancelled{CancelledBy: actor, Reason: reason}))
}

// Expire marks an item as expired. Typically called by a background
// job when the deadline has passed.
func (ib *Inbox) Expire(ctx context.Context, itemID string) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if IsTerminal(item.Status) {
		return Item{}, fmt.Errorf("inbox: item %s is already in terminal status %s", itemID, item.Status)
	}
	return ib.doTransition(ctx, item, StatusExpired,
		newTypedEvent("system", &inboxv1.ItemExpired{}))
}

// UpdatePayload replaces the payload on an item and records a
// PayloadUpdated event.
func (ib *Inbox) UpdatePayload(ctx context.Context, itemID string, payloadType string, payload json.RawMessage, actor string) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}

	item.PayloadType = payloadType
	item.Payload = payload

	evt := newTypedEvent(actor, &inboxv1.PayloadUpdated{PayloadType: payloadType})
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
		return Item{}, fmt.Errorf("inbox: update payload: %w", err)
	}

	return item, nil
}

// ─── Internal ───

// transition loads the item, validates the expected status, and writes
// the new status + event.
func (ib *Inbox) transition(ctx context.Context, itemID string, fromStatus string, toStatus string, evt Event) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if item.Status != fromStatus {
		return Item{}, fmt.Errorf("inbox: item %s has status %s, expected %s", itemID, item.Status, fromStatus)
	}
	return ib.doTransition(ctx, item, toStatus, evt)
}

// doTransition writes the status change + event to the entity store.
func (ib *Inbox) doTransition(ctx context.Context, item Item, toStatus string, evt Event) (Item, error) {
	oldStatus := item.Status
	item.Status = toStatus
	if evt.At.IsZero() {
		evt.At = time.Now().UTC()
	}
	item.Events = append(item.Events, evt)
	item.Tags = replaceStatusTag(item.Tags, oldStatus, toStatus)

	data, err := marshalItemData(item)
	if err != nil {
		return Item{}, fmt.Errorf("inbox: marshal item: %w", err)
	}

	_, err = ib.es.BatchWrite(ctx, []store.BatchWriteOp{
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
		return Item{}, fmt.Errorf("inbox: transition item: %w", err)
	}

	return item, nil
}
