package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/laenen-partners/entitystore/store"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/tags"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Claim transitions an item from open to claimed and sets the assignee tag atomically.
func (ib *Inbox) Claim(ctx context.Context, itemID string) (Item, error) {
	actor := actorFromCtx(ctx)
	assigneeTag, err := tags.Build("assignee", actor)
	if err != nil {
		return Item{}, fmt.Errorf("inbox: invalid actor for tag: %w", err)
	}
	evt := newProtoEvent(actor, &inboxv1.ItemClaimed{ClaimedBy: actor})
	return ib.transition(ctx, itemID, StatusOpen, StatusClaimed, evt, assigneeTag)
}

// Release transitions an item from claimed to open and removes the assignee tag.
func (ib *Inbox) Release(ctx context.Context, itemID string) (Item, error) {
	actor := actorFromCtx(ctx)
	evt := newProtoEvent(actor, &inboxv1.ItemReleased{ReleasedBy: actor})

	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if item.Proto.Status != StatusClaimed {
		return Item{}, fmt.Errorf("%w: item %s has status %s, expected %s",
			ErrInvalidTransition, itemID, item.Proto.Status, StatusClaimed)
	}
	// Remove assignee tag in the same write.
	item.Tags = item.Tags.Without("assignee")
	return ib.doTransition(ctx, item, StatusOpen, evt, nil)
}

// Close transitions an item to closed from any non-terminal status.
func (ib *Inbox) Close(ctx context.Context, itemID string, reason string) (Item, error) {
	actor := actorFromCtx(ctx)
	evt := newProtoEvent(actor, &inboxv1.ItemClosed{ClosedBy: actor, Reason: reason})

	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if IsTerminal(item.Status()) {
		return Item{}, fmt.Errorf("%w: %s", ErrTerminalStatus, item.Status())
	}
	return ib.doTransition(ctx, item, StatusClosed, evt, nil)
}

// ─── Internal helpers ───

// transition validates the expected status and delegates to doTransition.
// extraTags are added to the item's tag set atomically in the same write.
func (ib *Inbox) transition(ctx context.Context, itemID, fromStatus, toStatus string, evt *inboxv1.Event, extraTags ...string) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if item.Proto.Status != fromStatus {
		return Item{}, fmt.Errorf("%w: item %s has status %s, expected %s",
			ErrInvalidTransition, itemID, item.Proto.Status, fromStatus)
	}
	return ib.doTransition(ctx, item, toStatus, evt, extraTags)
}

// doTransition writes the status change + event to the entity store via BatchWrite.
func (ib *Inbox) doTransition(ctx context.Context, item Item, toStatus string, evt *inboxv1.Event, extraTags []string) (Item, error) {
	item.Proto.Status = toStatus
	if evt.GetAt() == nil {
		evt.At = timestamppb.New(time.Now().UTC())
	}
	item.Proto.Events = append(item.Proto.Events, evt)
	var tagErr error
	item.Tags, tagErr = item.Tags.With("status", toStatus)
	if tagErr != nil {
		return Item{}, fmt.Errorf("inbox: invalid status tag: %w", tagErr)
	}
	if len(extraTags) > 0 {
		item.Tags = item.Tags.Merge(tags.FromStrings(extraTags))
	}

	_, err := ib.es.BatchWrite(ctx, []store.BatchWriteOp{
		{
			WriteEntity: &store.WriteEntityOp{
				Action:          store.WriteActionUpdate,
				MatchedEntityID: item.ID,
				Data:            item.Proto,
				Tags:            item.Tags.Strings(),
			},
		},
	})
	if err != nil {
		return Item{}, fmt.Errorf("inbox: transition item: %w", err)
	}

	return item, nil
}
