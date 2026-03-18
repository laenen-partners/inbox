package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
)

// Tag adds tags to an item and records a tag change event.
func (ib *Inbox) Tag(ctx context.Context, itemID string, actor string, tags ...string) error {
	if err := ib.es.AddTags(ctx, itemID, tags); err != nil {
		return fmt.Errorf("inbox: add tags: %w", err)
	}

	_, _ = ib.AddEvent(ctx, itemID, newTypedEventWithDetail(actor, "+"+strings.Join(tags, ", +"), &inboxv1.TagsChanged{
		Added: tags,
	}))

	return nil
}

// Untag removes a tag from an item and records a tag change event.
func (ib *Inbox) Untag(ctx context.Context, itemID string, actor string, tag string) error {
	if err := ib.es.RemoveTag(ctx, itemID, tag); err != nil {
		return fmt.Errorf("inbox: remove tag: %w", err)
	}

	_, _ = ib.AddEvent(ctx, itemID, newTypedEventWithDetail(actor, "-"+tag, &inboxv1.TagsChanged{
		Removed: []string{tag},
	}))

	return nil
}

// TagsWithPrefix returns all tags matching the given prefix from an item.
// Example: TagsWithPrefix(item, "team:") returns ["team:finance"].
func TagsWithPrefix(item Item, prefix string) []string {
	var matches []string
	for _, t := range item.Tags {
		if strings.HasPrefix(t, prefix) {
			matches = append(matches, t)
		}
	}
	return matches
}

// TagValue returns the value part of the first tag matching the prefix.
// Example: TagValue(item, "priority:") returns "urgent".
// Returns empty string if no match.
func TagValue(item Item, prefix string) string {
	for _, t := range item.Tags {
		if strings.HasPrefix(t, prefix) {
			return t[len(prefix):]
		}
	}
	return ""
}

// HasTag reports whether the item has the given tag.
func HasTag(item Item, tag string) bool {
	for _, t := range item.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// MarshalTagChangeEvent creates event data JSON for a tag change.
// Use this when you want structured event data instead of the
// simple detail string that Tag/Untag use.
func MarshalTagChangeEvent(added, removed []string) json.RawMessage {
	data, _ := json.Marshal(struct {
		Added   []string `json:"added,omitempty"`
		Removed []string `json:"removed,omitempty"`
	}{Added: added, Removed: removed})
	return data
}
