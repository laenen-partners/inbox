package ui

import (
	"net/http"
	"time"

	"github.com/laenen-partners/inbox"
	"github.com/laenen-partners/tags"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleMyWork(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actor := actorStr(ctx)

	filterValues := s.readFilterValues(r)
	filterTags := []string{tags.Status(inbox.StatusClaimed), tags.Build("assignee", actor)}
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

	items, err := s.ib.ListByTags(ctx, filterTags, inbox.ListOpts{
		PageSize: 50,
		Cursor:   cursor,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := queueData{
		Items:    items,
		Filters:  s.cfg.filters,
		BasePath: s.cfg.basePath,
	}
	if len(items) == 50 {
		last := items[len(items)-1].UpdatedAt
		data.NextCursor = &last
	}

	// SSE fragment for Datastar filter/pagination, full page otherwise
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewSSE(w, r)
		sse.PatchElementTempl(queueTable(data))
		return
	}

	s.renderPage(w, r, "/mywork", myworkContent(data))
}
