package inbox

import (
	"context"
	"fmt"
	"strings"

	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/tags"
)

// Tag adds tags to an item and records a tag change event.
func (ib *Inbox) Tag(ctx context.Context, itemID string, rawTags ...string) error {
	actor := actorFromCtx(ctx)
	if err := ib.es.AddTags(ctx, itemID, rawTags); err != nil {
		return fmt.Errorf("inbox: add tags: %w", err)
	}

	_, _ = ib.AddEvent(ctx, itemID, newProtoEventWithDetail(actor, "+"+strings.Join(rawTags, ", +"), &inboxv1.TagsChanged{
		Added: rawTags,
	}))

	return nil
}

// Untag removes a tag from an item and records a tag change event.
func (ib *Inbox) Untag(ctx context.Context, itemID string, tag string) error {
	actor := actorFromCtx(ctx)
	if err := ib.es.RemoveTag(ctx, itemID, tag); err != nil {
		return fmt.Errorf("inbox: remove tag: %w", err)
	}

	_, _ = ib.AddEvent(ctx, itemID, newProtoEventWithDetail(actor, "-"+tag, &inboxv1.TagsChanged{
		Removed: []string{tag},
	}))

	return nil
}

// TagsWithPrefix returns all tags matching the given key prefix from an item.
// Example: TagsWithPrefix(item, "ref") returns a Set with all "ref:..." tags.
func TagsWithPrefix(item Item, prefix string) tags.Set {
	return item.Tags.WithPrefix(prefix)
}

// TagValue returns the value for the given tag key.
// Example: TagValue(item, "priority") returns "urgent".
// Returns empty string if the key is not present.
func TagValue(item Item, key string) string {
	return item.Tags.Value(key)
}

// HasTag reports whether the item has the given tag (key:value pair).
func HasTag(item Item, tag string) bool {
	return item.Tags.HasAll(tags.FromStrings([]string{tag}))
}
