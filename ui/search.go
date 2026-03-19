package ui

import (
	"net/http"

	"github.com/laenen-partners/inbox"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query().Get("q")

	var items []inbox.Item
	var err error

	if query != "" {
		// Note: Search backend does not support cursor-based pagination
		items, err = s.ib.Search(ctx, query, inbox.ListOpts{
			PageSize: 50,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	data := searchData{
		Query:    query,
		Items:    items,
		BasePath: s.cfg.basePath,
	}

	// SSE fragment for Datastar search submit, full page otherwise
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewSSE(w, r)
		sse.PatchElementTempl(queueTable(queueData{
			Items:    data.Items,
			BasePath: data.BasePath,
		}))
		return
	}

	s.renderPage(w, r, "/search", searchContent(data))
}

type searchData struct {
	Query    string
	Items    []inbox.Item
	BasePath string
}
