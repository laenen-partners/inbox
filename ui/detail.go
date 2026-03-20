package ui

import (
	"context"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/laenen-partners/dsx/ds"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
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
// rendering to the provider. Otherwise it falls back to legacy schema/payload rendering.
func (s *server) buildDetailData(ctx context.Context, item inbox.Item, actor string) detailData {
	data := detailData{
		Item:     item,
		Actor:    actor,
		BasePath: s.cfg.basePath,
	}

	// Try ContentProvider first
	if provider, ok := s.cfg.contentProviders[item.PayloadType()]; ok {
		rc := RenderContext{Item: item, Actor: actor, BasePath: s.cfg.basePath}
		data.Content = provider.Render(ctx, rc)
	} else {
		// Legacy fallback: try to parse as ItemSchema (renders interactive form)
		if item.Proto.GetPayload() != nil {
			data.Schema = tryParseSchema(item.PayloadType(), item.Proto.GetPayload().GetValue())
		}
		data.CanLink = s.cfg.signer != nil && data.Schema != nil && data.Schema.ClientCompletable
		if data.Schema == nil {
			if fn, ok := s.cfg.payloadRenderers[item.PayloadType()]; ok {
				if item.Proto.GetPayload() != nil {
					data.PayloadComponent = fn(item.PayloadType(), item.Proto.GetPayload().GetValue())
				}
			}
		}
	}

	assignee := inbox.TagValue(item, "assignee")
	data.IsClaimant = assignee == actor
	return data
}

type detailData struct {
	Item             inbox.Item
	Actor            string
	IsClaimant       bool
	BasePath         string
	Content          templ.Component
	PayloadComponent templ.Component
	Schema           *inboxv1.ItemSchema
	CanLink          bool
}
