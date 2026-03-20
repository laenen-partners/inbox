package ui

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/laenen-partners/dsx/ds"
	"github.com/laenen-partners/inbox"
	"github.com/laenen-partners/tags"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleClaim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	actor := actorStr(ctx)

	_, err := s.ib.Claim(ctx, id)
	if err != nil {
		s.sseError(w, r, err)
		return
	}
	if err := s.ib.Tag(ctx, id, tags.Build("assignee", actor)); err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Item claimed")
}

func (s *server) handleRelease(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	item, err := s.ib.Release(ctx, id)
	if err != nil {
		s.sseError(w, r, err)
		return
	}
	if assignee := inbox.TagValue(item, "assignee"); assignee != "" {
		_ = s.ib.Untag(ctx, id, tags.Build("assignee", assignee))
	}

	s.refreshDetailAndToast(w, r, id, "Item released")
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

	_, err := s.ib.Respond(ctx, id, inbox.Response{
		Action:  signals.Action,
		Comment: signals.Comment,
	})
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Response recorded")
}

func (s *server) handleComplete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	_, err := s.ib.Complete(ctx, id)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Item completed")
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

	_, err := s.ib.Cancel(ctx, id, signals.Reason)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Item cancelled")
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

	_, err := s.ib.Comment(ctx, id, signals.Body, nil)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Comment added")
}

// refreshDetailAndToast re-renders the detail drawer, updates the queue row, and shows a toast.
func (s *server) refreshDetailAndToast(w http.ResponseWriter, r *http.Request, id string, msg string) {
	ctx := r.Context()
	actor := actorStr(ctx)

	item, err := s.ib.Get(ctx, id)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	data := s.buildDetailData(ctx, item, actor)

	sse := datastar.NewSSE(w, r)
	ds.Send.Drawer(sse, detailDrawer(data))
	sse.PatchElementTempl(queueRow(item, s.cfg.basePath))
	ds.Send.Toast(sse, ds.ToastSuccess, msg)
}

func (s *server) sseError(w http.ResponseWriter, r *http.Request, err error) {
	sse := datastar.NewSSE(w, r)
	ds.Send.Toast(sse, ds.ToastError, err.Error())
}
