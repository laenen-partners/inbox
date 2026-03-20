package ui

import (
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/tags"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filterValues := s.readFilterValues(r)

	filterTags := []string{tags.Status(inbox.StatusOpen)}
	for _, f := range s.cfg.filters {
		if v := filterValues[filterKey(f.TagPrefix)]; v != "" {
			filterTags = append(filterTags, f.TagPrefix+v)
		}
	}

	var cursor *time.Time
	if c := r.URL.Query().Get("cursor"); c != "" {
		if t, err := time.Parse(time.RFC3339, c); err == nil {
			cursor = &t
		}
	}

	resp, err := s.client.ListItems(ctx, connect.NewRequest(&inboxv1.ListItemsRequest{
		Identity: identityToProto(ctx),
		Tags:     filterTags,
		PageSize: 50,
		Cursor:   cursorToProto(cursor),
	}))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := fromProtoSlice(resp.Msg.Items)

	// Build active filters for display
	activeFilters := make(map[string]string)
	for _, f := range s.cfg.filters {
		if v := filterValues[filterKey(f.TagPrefix)]; v != "" {
			activeFilters[f.Label] = v
		}
	}

	// Count by priority for stat cards
	counts := make(map[string]int)
	for _, item := range items {
		p := inbox.TagValue(item, "priority")
		if p != "" {
			counts[p]++
		}
	}

	data := queueData{
		Items:          items,
		Filters:        s.cfg.filters,
		ActiveFilters:  activeFilters,
		BasePath:       s.cfg.basePath,
		PriorityCounts: counts,
	}

	if resp.Msg.NextCursor != nil {
		t := resp.Msg.NextCursor.AsTime()
		data.NextCursor = &t
	}

	// SSE fragment for Datastar filter/pagination, full page otherwise
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewSSE(w, r)
		_ = sse.PatchElementTempl(queueTable(data))
		return
	}

	s.renderPage(w, r, "/", queueContent(data))
}

type queueData struct {
	Items          []inbox.Item
	Filters        []FilterConfig
	ActiveFilters  map[string]string
	BasePath       string
	NextCursor     *time.Time
	PriorityCounts map[string]int
}
