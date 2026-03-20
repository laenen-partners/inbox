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

// AddEvent appends an event to an item's event log.
// This is the general-purpose way to add context to an item without
// changing its status. Use the convenience methods (Comment, Escalate,
// Reassign) for common event types with typed data.
func (ib *Inbox) AddEvent(ctx context.Context, itemID string, evt *inboxv1.Event) (Item, error) {
	item, err := ib.Get(ctx, itemID)
	if err != nil {
		return Item{}, err
	}

	if evt.GetAt() == nil {
		evt.At = timestamppb.New(time.Now().UTC())
	}
	item.Proto.Events = append(item.Proto.Events, evt)

	_, err = ib.es.BatchWrite(ctx, []store.BatchWriteOp{
		{
			WriteEntity: &store.WriteEntityOp{
				Action:          store.WriteActionUpdate,
				MatchedEntityID: itemID,
				Data:            item.Proto,
			},
		},
	})
	if err != nil {
		return Item{}, fmt.Errorf("inbox: add event: %w", err)
	}

	return item, nil
}

// ─── Standard event convenience methods ───

// Comment adds a comment to an item. Comments are the primary way
// humans and agents add context without changing item state.
func (ib *Inbox) Comment(ctx context.Context, itemID string, body string, opts *CommentOpts) (Item, error) {
	actor := actorFromCtx(ctx)
	evtData := &inboxv1.CommentAppended{Body: body}
	if opts != nil {
		evtData.Visibility = opts.Visibility
		evtData.Refs = opts.Refs
	}

	return ib.AddEvent(ctx, itemID, newProtoEventWithDetail(actor, body, evtData))
}

// Escalate moves an item from one team to another with a reason.
// Updates team tags and records an ItemEscalated event.
func (ib *Inbox) Escalate(ctx context.Context, itemID string, fromTeam string, toTeam string, reason string) (Item, error) {
	actor := actorFromCtx(ctx)
	if fromTeam != "" {
		_ = ib.es.RemoveTag(ctx, itemID, tags.Team(fromTeam))
	}
	if toTeam != "" {
		_ = ib.es.AddTags(ctx, itemID, []string{tags.Team(toTeam)})
	}

	return ib.AddEvent(ctx, itemID, newProtoEventWithDetail(actor, reason, &inboxv1.ItemEscalated{
		FromTeam: fromTeam,
		ToTeam:   toTeam,
		Reason:   reason,
	}))
}

// Reassign moves an item from one actor to another.
// Updates assignee tags and records an ItemReassigned event.
func (ib *Inbox) Reassign(ctx context.Context, itemID string, fromActor string, toActor string, reason string) (Item, error) {
	actor := actorFromCtx(ctx)
	if fromActor != "" {
		_ = ib.es.RemoveTag(ctx, itemID, tags.Build("assignee", fromActor))
	}
	if toActor != "" {
		_ = ib.es.AddTags(ctx, itemID, []string{tags.Build("assignee", toActor)})
	}

	return ib.AddEvent(ctx, itemID, newProtoEventWithDetail(actor, reason, &inboxv1.ItemReassigned{
		FromActor: fromActor,
		ToActor:   toActor,
		Reason:    reason,
	}))
}
