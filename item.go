package inbox

import (
	"encoding/json"
	"fmt"
	"time"

	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/laenen-partners/entitystore/matching"
)

// Item is the domain representation of an inbox item.
// It wraps the generated proto Item with entity store metadata.
type Item struct {
	ID        string
	Proto     *inboxv1.Item
	Tags      []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Convenience accessors.
func (it Item) Title() string            { return it.Proto.GetTitle() }
func (it Item) Description() string      { return it.Proto.GetDescription() }
func (it Item) Status() string           { return it.Proto.GetStatus() }
func (it Item) PayloadType() string      { return it.Proto.GetPayloadType() }
func (it Item) Events() []*inboxv1.Event { return it.Proto.GetEvents() }
func (it Item) IdempotencyKey() string   { return it.Proto.GetIdempotencyKey() }

func (it Item) Deadline() *time.Time {
	if it.Proto.GetDeadline() != nil {
		t := it.Proto.GetDeadline().AsTime()
		return &t
	}
	return nil
}

// Meta describes an inbox item to be created.
type Meta struct {
	Title          string
	Description    string
	Deadline       *time.Time
	Payload        proto.Message
	Tags           []string
	Actor          string
	IdempotencyKey string
}

// Response is the data sent when responding to an item.
type Response struct {
	Actor   string
	Action  string
	Comment string
	Data    json.RawMessage
}

// ListOpts configures list/search queries.
type ListOpts struct {
	PageSize int
	Cursor   *time.Time
}

// CommentOpts configures a comment event.
type CommentOpts struct {
	Visibility []string
	Refs       []string
}

// ─── Internal helpers ───

func itemFromEntity(e matching.StoredEntity) (Item, error) {
	p := &inboxv1.Item{}
	if err := e.GetData(p); err != nil {
		return Item{}, fmt.Errorf("inbox: unmarshal item: %w", err)
	}
	return Item{
		ID:        e.ID,
		Proto:     p,
		Tags:      e.Tags,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}, nil
}

func itemsFromEntities(entities []matching.StoredEntity) ([]Item, error) {
	items := make([]Item, 0, len(entities))
	for _, e := range entities {
		item, err := itemFromEntity(e)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func deadlineToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func packPayloadAny(msg proto.Message) *anypb.Any {
	if msg == nil {
		return nil
	}
	a, err := anypb.New(msg)
	if err != nil {
		return nil
	}
	return a
}

func payloadTypeFromMsg(msg proto.Message) string {
	if msg == nil {
		return ""
	}
	return string(proto.MessageName(msg))
}

// ─── Public payload helpers ───

// UnpackPayload deserializes a proto Any into a concrete proto message.
func UnpackPayload[T proto.Message](a *anypb.Any, target T) error {
	if a == nil {
		return nil
	}
	return a.UnmarshalTo(target)
}
