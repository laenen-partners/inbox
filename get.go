package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/laenen-partners/entitystore/matching"
	"github.com/laenen-partners/tags"
)

// Get returns a single inbox item by ID.
func (ib *Inbox) Get(ctx context.Context, itemID string) (Item, error) {
	e, err := ib.es.GetEntity(ctx, itemID)
	if err != nil {
		return Item{}, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if e.EntityType != EntityType {
		return Item{}, fmt.Errorf("%w: entity %s is not an inbox item", ErrNotFound, itemID)
	}
	return itemFromEntity(e)
}

// ListByTags returns inbox items matching all given tags.
func (ib *Inbox) ListByTags(ctx context.Context, tags []string, opts ListOpts) ([]Item, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	entities, err := ib.es.GetEntitiesByType(ctx, EntityType, int32(pageSize), opts.Cursor, nil)
	if err != nil {
		return nil, fmt.Errorf("inbox: list items: %w", err)
	}
	// Filter by tags client-side (entity store GetEntitiesByType doesn't
	// take a tag filter — we use FindByTokens with a QueryFilter for that).
	// For tag-only queries, use the entity store's tag-filtered search.
	if len(tags) > 0 {
		entities = filterByTags(entities, tags)
	}
	return itemsFromEntities(entities)
}

// Search performs fuzzy text search across item titles and descriptions.
func (ib *Inbox) Search(ctx context.Context, query string, opts ListOpts) ([]Item, error) {
	limit := opts.PageSize
	if limit <= 0 {
		limit = 50
	}
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}
	entities, err := ib.es.FindByTokens(ctx, EntityType, tokens, limit, nil)
	if err != nil {
		return nil, fmt.Errorf("inbox: search items: %w", err)
	}
	return itemsFromEntities(entities)
}

// SemanticSearch finds items by vector similarity on title + description embeddings.
func (ib *Inbox) SemanticSearch(ctx context.Context, vec []float32, topK int) ([]Item, error) {
	if topK <= 0 {
		topK = 10
	}
	entities, err := ib.es.FindByEmbedding(ctx, EntityType, vec, topK, nil)
	if err != nil {
		return nil, fmt.Errorf("inbox: semantic search: %w", err)
	}
	return itemsFromEntities(entities)
}

// Stale returns items matching tags where the last event is older than age.
// Useful for building reminder and escalation policies.
func (ib *Inbox) Stale(ctx context.Context, tags []string, age time.Duration, opts ListOpts) ([]Item, error) {
	items, err := ib.ListByTags(ctx, tags, opts)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-age)
	var stale []Item
	for _, item := range items {
		if lastEventBefore(item, cutoff) {
			stale = append(stale, item)
		}
	}
	return stale, nil
}

// ─── Internal helpers ───

func filterByTags(entities []matching.StoredEntity, required []string) []matching.StoredEntity {
	req := tags.MustNew(required...)
	var filtered []matching.StoredEntity
	for _, e := range entities {
		if tags.FromStrings(e.Tags).HasAll(req) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func lastEventBefore(item Item, cutoff time.Time) bool {
	events := item.Proto.GetEvents()
	if len(events) == 0 {
		return item.CreatedAt.Before(cutoff)
	}
	last := events[len(events)-1]
	if last.GetAt() == nil {
		return item.CreatedAt.Before(cutoff)
	}
	return last.GetAt().AsTime().Before(cutoff)
}
