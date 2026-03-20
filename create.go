package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/laenen-partners/entitystore/matching"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/identity"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Create inserts a new inbox item into the entity store and returns the created item.
// The item is created with status "open" and a "created" event is recorded.
//
// Tags are set on the entity store entity (not in JSONB data). The caller
// should include routing tags like "type:approval", "team:compliance", etc.
// A "status:open" tag is added automatically.
func (ib *Inbox) Create(ctx context.Context, meta Meta) (Item, error) {
	now := time.Now().UTC()
	id := uuid.NewString()
	actor := actorFromCtx(ctx)

	createdEvt := newProtoEvent(actor, &inboxv1.ItemCreated{PayloadType: payloadTypeFromMsg(meta.Payload)})
	createdEvt.At = timestamppb.New(now)

	tags := appendStatusTag(meta.Tags, StatusOpen)
	if meta.Deadline != nil {
		tags = append(tags, "deadline:"+meta.Deadline.Format(time.RFC3339))
	}

	p := &inboxv1.Item{
		IdempotencyKey: meta.IdempotencyKey,
		Title:          meta.Title,
		Description:    meta.Description,
		Status:         StatusOpen,
		Deadline:       deadlineToProto(meta.Deadline),
		PayloadType:    payloadTypeFromMsg(meta.Payload),
		Payload:        packPayloadAny(meta.Payload),
		Events:         []*inboxv1.Event{createdEvt},
	}

	tokens := buildTokens(p.Title, p.Description)

	writeOp := store.WriteEntityOp{
		Action: store.WriteActionCreate,
		ID:     id,
		Data:   p,
		Tags:   tags,
		Tokens: tokens,
	}

	// Set anchor for idempotency if key is provided.
	if meta.IdempotencyKey != "" {
		writeOp.Anchors = []matching.AnchorQuery{
			{Field: "idempotency_key", Value: meta.IdempotencyKey},
		}
	}

	_, err := ib.es.BatchWrite(ctx, []store.BatchWriteOp{
		{WriteEntity: &writeOp},
	})
	if err != nil {
		return Item{}, fmt.Errorf("inbox: create item: %w", err)
	}

	return Item{
		ID:        id,
		Proto:     p,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ─── Proto event helpers ───

// newProtoEvent creates an *inboxv1.Event from a proto message. The DataType
// field is derived automatically from the proto message's fully qualified name.
func newProtoEvent(actor string, msg proto.Message) *inboxv1.Event {
	a, _ := anypb.New(msg)
	return &inboxv1.Event{
		Actor:    actor,
		DataType: string(proto.MessageName(msg)),
		Data:     a,
	}
}

// newProtoEventWithDetail creates an event with an additional detail string.
func newProtoEventWithDetail(actor, detail string, msg proto.Message) *inboxv1.Event {
	evt := newProtoEvent(actor, msg)
	evt.Detail = detail
	return evt
}

// ─── Internal helpers ───

func actorFromCtx(ctx context.Context) string {
	id := identity.MustFromContext(ctx)
	return string(id.PrincipalType()) + ":" + id.PrincipalID()
}

// appendStatusTag ensures a "status:<s>" tag is present.
func appendStatusTag(tags []string, s string) []string {
	tag := "status:" + s
	for _, t := range tags {
		if t == tag {
			return tags
		}
	}
	return append(tags, tag)
}

// buildTokens creates the token map for fuzzy text search.
func buildTokens(title, description string) map[string][]string {
	tokens := make(map[string][]string)
	if t := tokenize(title); len(t) > 0 {
		tokens["title"] = t
	}
	if t := tokenize(description); len(t) > 0 {
		tokens["description"] = t
	}
	return tokens
}

// tokenize splits text into lowercase tokens for fuzzy search.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	var tokens []string
	seen := make(map[string]bool)
	word := make([]byte, 0, 32)
	for i := range len(s) {
		c := s[i]
		if isAlphaNum(c) {
			word = append(word, lower(c))
		} else if len(word) > 0 {
			t := string(word)
			if !seen[t] {
				tokens = append(tokens, t)
				seen[t] = true
			}
			word = word[:0]
		}
	}
	if len(word) > 0 {
		t := string(word)
		if !seen[t] {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

// findCallbackTag extracts the callback address from item tags.
func findCallbackTag(tags []string) string {
	const prefix = "callback:"
	for _, t := range tags {
		if len(t) > len(prefix) && t[:len(prefix)] == prefix {
			return t[len(prefix):]
		}
	}
	return ""
}

// replaceStatusTag swaps the old status tag for the new one.
func replaceStatusTag(tags []string, oldStatus, newStatus string) []string {
	oldTag := "status:" + oldStatus
	newTag := "status:" + newStatus
	result := make([]string, 0, len(tags))
	found := false
	for _, t := range tags {
		if t == oldTag {
			result = append(result, newTag)
			found = true
		} else {
			result = append(result, t)
		}
	}
	if !found {
		result = append(result, newTag)
	}
	return result
}
