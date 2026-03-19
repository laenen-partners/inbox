package ui

import (
	"net/http"
	"time"

	"github.com/laenen-partners/inbox"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tags := []string{"status:open"}
	for _, f := range s.cfg.filters {
		if v := r.URL.Query().Get(f.TagPrefix); v != "" {
			tags = append(tags, f.TagPrefix+v)
		}
	}

	var cursor *time.Time
	if c := r.URL.Query().Get("cursor"); c != "" {
		if t, err := time.Parse(time.RFC3339, c); err == nil {
			cursor = &t
		}
	}

	items, err := s.ib.ListByTags(ctx, tags, inbox.ListOpts{
		PageSize: 50,
		Cursor:   cursor,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build active filters for display
	activeFilters := make(map[string]string)
	for _, f := range s.cfg.filters {
		if v := r.URL.Query().Get(f.TagPrefix); v != "" {
			activeFilters[f.Label] = v
		}
	}

	data := queueData{
		Items:         items,
		Filters:       s.cfg.filters,
		ActiveFilters: activeFilters,
		BasePath:      s.cfg.basePath,
	}

	// If next page exists, set cursor
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

	s.renderPage(w, r, "/", queueContent(data))
}

type queueData struct {
	Items         []inbox.Item
	Filters       []FilterConfig
	ActiveFilters map[string]string
	BasePath      string
	NextCursor    *time.Time
}
