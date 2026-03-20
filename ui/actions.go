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
	item := fromProto(resp.Msg.Item)

	s.renderDetailAndToast(w, r, item, "Item claimed")
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
	item := fromProto(resp.Msg.Item)

	s.renderDetailAndToast(w, r, item, "Item released")
}

func (s *server) handleRespond(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var signals struct {
		Action  string `json:"action"`
		Comment string `json:"comment"`
	}
	if err := ds.ReadSignals("respond-form", r, &signals); err != nil {
		s.sseError(w, r, err)
		return
	}

	resp, err := s.client.RespondToItem(ctx, connect.NewRequest(&inboxv1.RespondToItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
		Action:   signals.Action,
		Comment:  signals.Comment,
	}))
	if err != nil {
		s.sseError(w, r, err)
		return
	}
	item := fromProto(resp.Msg.Item)

	s.renderDetailAndToast(w, r, item, "Response recorded")
}

func (s *server) handleComplete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	resp, err := s.client.CompleteItem(ctx, connect.NewRequest(&inboxv1.CompleteItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
	}))
	if err != nil {
		s.sseError(w, r, err)
		return
	}
	item := fromProto(resp.Msg.Item)

	s.renderDetailAndToast(w, r, item, "Item completed")
}

func (s *server) handleCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var signals struct {
		Reason string `json:"reason"`
	}
	if err := ds.ReadSignals("cancel-form", r, &signals); err != nil {
		s.sseError(w, r, err)
		return
	}

	resp, err := s.client.CancelItem(ctx, connect.NewRequest(&inboxv1.CancelItemRequest{
		Identity: identityToProto(ctx),
		Id:       id,
		Reason:   signals.Reason,
	}))
	if err != nil {
		s.sseError(w, r, err)
		return
	}
	item := fromProto(resp.Msg.Item)

	s.renderDetailAndToast(w, r, item, "Item cancelled")
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
	item := fromProto(resp.Msg.Item)

	s.renderDetailAndToast(w, r, item, "Comment added")
}

// renderDetailAndToast re-renders the detail drawer, updates the queue row, and shows a toast.
func (s *server) renderDetailAndToast(w http.ResponseWriter, r *http.Request, item inbox.Item, msg string) {
	actor := actorStr(r.Context())
	data := s.buildDetailData(r.Context(), item, actor)
	sse := datastar.NewSSE(w, r)
	_ = ds.Send.Drawer(sse, detailDrawer(data))
	_ = sse.PatchElementTempl(queueRow(item, s.cfg.basePath))
	_ = ds.Send.Toast(sse, ds.ToastSuccess, msg)
}

func (s *server) sseError(w http.ResponseWriter, r *http.Request, err error) {
	sse := datastar.NewSSE(w, r)
	_ = ds.Send.Toast(sse, ds.ToastError, err.Error())
}
