package ui

import (
	"context"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/laenen-partners/dsx/ds"
	"github.com/laenen-partners/inbox"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	actor := actorStr(ctx)

	item, err := s.ib.Get(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	data := s.buildDetailData(ctx, item, actor)
	sse := datastar.NewSSE(w, r)
	ds.Send.Drawer(sse, detailDrawer(data))
}

// buildDetailData constructs the detailData for rendering the detail drawer.
// When a ContentProvider is registered for the item's payload type, it delegates
// rendering to the provider.
func (s *server) buildDetailData(ctx context.Context, item inbox.Item, actor string) detailData {
	data := detailData{
		Item:     item,
		Actor:    actor,
		BasePath: s.cfg.basePath,
	}
	if provider, ok := s.cfg.contentProviders[item.PayloadType()]; ok {
		rc := RenderContext{Item: item, Actor: actor, BasePath: s.cfg.basePath}
		data.Content = provider.Render(ctx, rc)
	}
	assignee := inbox.TagValue(item, "assignee")
	data.IsClaimant = assignee == actor
	return data
}

type detailData struct {
	Item       inbox.Item
	Actor      string
	IsClaimant bool
	BasePath   string
	Content    templ.Component
}
