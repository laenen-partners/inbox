package ui

import (
	"net/http"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/laenen-partners/dsx/ds"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleClaim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	resp, err := s.client.ClaimItem(ctx, connect.NewRequest(&inboxv1.ClaimItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
	}))
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.respondWithDetail(w, r, fromProto(resp.Msg.Item), "Item claimed")
}

func (s *server) handleRelease(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	resp, err := s.client.ReleaseItem(ctx, connect.NewRequest(&inboxv1.ReleaseItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
	}))
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.respondWithDetail(w, r, fromProto(resp.Msg.Item), "Item released")
}

func (s *server) handleClose(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var signals struct {
		Reason string `json:"reason"`
	}
	_ = ds.ReadSignals("close-form", r, &signals)
	if signals.Reason == "" {
		signals.Reason = "closed"
	}

	resp, err := s.client.CloseItem(ctx, connect.NewRequest(&inboxv1.CloseItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
		Reason:   signals.Reason,
	}))
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.respondWithDetail(w, r, fromProto(resp.Msg.Item), "Item closed")
}

func (s *server) handleComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var signals struct {
		Body string `json:"body"`
	}
	if err := ds.ReadSignals("comment-form", r, &signals); err != nil {
		s.sseError(w, r, err)
		return
	}

	resp, err := s.client.CommentOnItem(ctx, connect.NewRequest(&inboxv1.CommentOnItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
		Body:     signals.Body,
	}))
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.respondWithDetail(w, r, fromProto(resp.Msg.Item), "Comment added")
}

// respondWithDetail re-renders the detail drawer, updates the queue row,
// publishes a bus notification, and shows a toast.
func (s *server) respondWithDetail(w http.ResponseWriter, r *http.Request, item inbox.Item, msg string) {
	actor := actorStr(r.Context())
	data := s.buildDetailData(r.Context(), item, actor)
	sse := datastar.NewSSE(w, r)
	_ = ds.Send.Drawer(sse, detailDrawer(data))
	_ = sse.PatchElementTempl(queueRow(item, s.cfg.basePath))
	_ = ds.Send.Toast(sse, ds.ToastSuccess, msg)
}

// handleDetailReload re-renders the detail drawer for stream-triggered reloads.
func (s *server) handleDetailReload(w http.ResponseWriter, r *http.Request) {
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
	_ = ds.Send.Drawer(sse, detailDrawer(data))
}

// handleRowReload re-renders a single queue row for stream-triggered reloads.
func (s *server) handleRowReload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	resp, err := s.client.GetItem(ctx, connect.NewRequest(&inboxv1.GetItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
	}))
	if err != nil {
		return // silently skip — row may have been deleted
	}
	item := fromProto(resp.Msg.Item)

	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(queueRow(item, s.cfg.basePath))
}

func (s *server) sseError(w http.ResponseWriter, r *http.Request, err error) {
	sse := datastar.NewSSE(w, r)
	_ = ds.Send.Toast(sse, ds.ToastError, err.Error())
}
