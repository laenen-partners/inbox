package ui

import (
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

	data := detailData{
		Item:     item,
		Actor:    actor,
		BasePath: s.cfg.basePath,
	}

	// Try to parse as ItemSchema first (renders interactive form)
	if item.Proto.GetPayload() != nil {
		data.Schema = tryParseSchema(item.PayloadType(), item.Proto.GetPayload().GetValue())
	}

	// Only show "Send as Link" if signer is configured AND schema is client-completable
	data.CanLink = s.cfg.signer != nil && data.Schema != nil && data.Schema.ClientCompletable

	// Fall back to custom payload renderer
	if data.Schema == nil {
		if fn, ok := s.cfg.payloadRenderers[item.PayloadType()]; ok {
			if item.Proto.GetPayload() != nil {
				data.PayloadComponent = fn(item.PayloadType(), item.Proto.GetPayload().GetValue())
			}
		}
	}

	// Determine if current actor is the claimant
	assignee := inbox.TagValue(item, "assignee:")
	data.IsClaimant = assignee == actor

	sse := datastar.NewSSE(w, r)
	ds.Send.Drawer(sse, detailDrawer(data))
}

type detailData struct {
	Item             inbox.Item
	Actor            string
	IsClaimant       bool
	BasePath         string
	PayloadComponent templ.Component
	Schema           *inboxv1.ItemSchema
	CanLink          bool
}
