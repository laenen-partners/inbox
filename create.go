package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/laenen-partners/entitystore/matching"
	"github.com/laenen-partners/entitystore/store"
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
	actor := meta.Actor
	if actor == "" {
		actor = "system"
	}

	createdEvt := newTypedEvent(actor, ActionCreated, "", TypeItemCreated, &ItemCreated{PayloadType: meta.PayloadType})
	createdEvt.At = now

	item := Item{
		ID:             id,
		IdempotencyKey: meta.IdempotencyKey,
		Title:          meta.Title,
		Description:    meta.Description,
		Status:         StatusOpen,
		Deadline:       meta.Deadline,
		PayloadType:    meta.PayloadType,
		Payload:        meta.Payload,
		Events:         []Event{createdEvt},
		Tags:           appendStatusTag(meta.Tags, StatusOpen),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if meta.Deadline != nil {
		item.Tags = append(item.Tags, "deadline:"+meta.Deadline.Format(time.RFC3339))
	}

	data, err := marshalItemData(item)
	if err != nil {
		return Item{}, fmt.Errorf("inbox: marshal item: %w", err)
	}

	tokens := buildTokens(item.Title, item.Description)

	writeOp := store.WriteEntityOp{
		Action:     store.WriteActionCreate,
		ID:         id,
		EntityType: EntityType,
		Data:       data,
		Tags:       item.Tags,
		Tokens:     tokens,
	}

	// Set anchor for idempotency if key is provided.
	if meta.IdempotencyKey != "" {
		writeOp.Anchors = []matching.AnchorQuery{
			{Field: "idempotency_key", Value: meta.IdempotencyKey},
		}
	}

	_, err = ib.es.BatchWrite(ctx, []store.BatchWriteOp{
		{WriteEntity: &writeOp},
	})
	if err != nil {
		return Item{}, fmt.Errorf("inbox: create item: %w", err)
	}

	return item, nil
}

// ─── Internal helpers ───

// itemData is the JSONB data shape stored in the entity store.
// Tags and ID live outside JSONB (entity store columns).
type itemData struct {
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Status         string          `json:"status"`
	Deadline       *time.Time      `json:"deadline,omitempty"`
	PayloadType    string          `json:"payload_type,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Events         []Event         `json:"events,omitempty"`
}

func marshalItemData(item Item) (json.RawMessage, error) {
	return json.Marshal(itemData{
		IdempotencyKey: item.IdempotencyKey,
		Title:          item.Title,
		Description:    item.Description,
		Status:         item.Status,
		Deadline:       item.Deadline,
		PayloadType:    item.PayloadType,
		Payload:        item.Payload,
		Events:         item.Events,
	})
}

func unmarshalItem(e matching.StoredEntity) (Item, error) {
	var d itemData
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return Item{}, fmt.Errorf("inbox: unmarshal item data: %w", err)
	}
	return Item{
		ID:             e.ID,
		IdempotencyKey: d.IdempotencyKey,
		Title:          d.Title,
		Description:    d.Description,
		Status:         d.Status,
		Deadline:       d.Deadline,
		PayloadType:    d.PayloadType,
		Payload:        d.Payload,
		Events:         d.Events,
		Tags:        e.Tags,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}, nil
}

func unmarshalItems(entities []matching.StoredEntity) ([]Item, error) {
	items := make([]Item, 0, len(entities))
	for _, e := range entities {
		item, err := unmarshalItem(e)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
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
