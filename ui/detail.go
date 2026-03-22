package ui

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
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

	resp, err := s.client.GetItem(ctx, connect.NewRequest(&inboxv1.GetItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
	}))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	item := fromProto(resp.Msg.Item)

	data := s.buildDetailData(ctx, item, actor)
	sse := datastar.NewSSE(w, r)

	var drawerOpts []ds.DrawerOption
	if data.DrawerSize == DrawerSizeWide {
		drawerOpts = append(drawerOpts, ds.WithDrawerMaxWidth("max-w-2xl"))
	}
	_ = ds.Send.Drawer(sse, detailDrawer(data), drawerOpts...)
}

// buildDetailData constructs the detailData for rendering the detail drawer.
// When a ContentProvider is registered for the item's payload type, it delegates
// rendering to the provider.
func (s *server) buildDetailData(ctx context.Context, item inbox.Item, actor string) detailData {
	data := detailData{
		Item:       item,
		Actor:      actor,
		IsClaimant: inbox.TagValue(item, "assignee") == actor,
		BasePath:   s.cfg.basePath,
	}

	provider, ok := s.cfg.contentProviders[item.PayloadType()]
	if !ok {
		provider = s.cfg.contentProviders[""]
	}
	if provider == nil {
		return data
	}

	rc := RenderContext{Item: item, Actor: actor, BasePath: s.cfg.basePath}

	// Check for RichContentProvider first.
	if rich, ok := provider.(RichContentProvider); ok {
		result := rich.RenderRich(ctx, rc)
		data.Content = result.Content
		data.DrawerSize = result.Size
		data.Links = result.Links
	} else {
		data.Content = provider.Render(ctx, rc)
	}

	// Check for WorkflowStatusProvider.
	if wsp, ok := provider.(WorkflowStatusProvider); ok {
		if state, err := wsp.WorkflowStatus(ctx, item); err == nil {
			data.WorkflowState = &state
		}
	}

	return data
}

type detailData struct {
	Item          inbox.Item
	Actor         string
	IsClaimant    bool
	BasePath      string
	Content       templ.Component
	DrawerSize    DrawerSize
	Links         []Link
	WorkflowState *WorkflowState
}
