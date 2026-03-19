package ui

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/laenen-partners/dsx/ds"
	"github.com/laenen-partners/inbox"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleClaim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	actor := inbox.ActorFrom(ctx)

	_, err := s.ib.Claim(ctx, id, actor)
	if err != nil {
		s.sseError(w, r, err)
		return
	}
	if err := s.ib.Tag(ctx, id, actor, "assignee:"+actor); err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Item claimed")
}

func (s *server) handleRelease(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	actor := inbox.ActorFrom(ctx)

	item, err := s.ib.Release(ctx, id, actor)
	if err != nil {
		s.sseError(w, r, err)
		return
	}
	if assignee := inbox.TagValue(item, "assignee:"); assignee != "" {
		_ = s.ib.Untag(ctx, id, actor, "assignee:"+assignee)
	}

	s.refreshDetailAndToast(w, r, id, "Item released")
}

func (s *server) handleRespond(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	actor := inbox.ActorFrom(ctx)

	var signals struct {
		Action  string `json:"action"`
		Comment string `json:"comment"`
	}
	if err := ds.ReadSignals("respond-form", r, &signals); err != nil {
		s.sseError(w, r, err)
		return
	}

	_, err := s.ib.Respond(ctx, id, inbox.Response{
		Actor:   actor,
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
	actor := inbox.ActorFrom(ctx)

	_, err := s.ib.Complete(ctx, id, actor)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Item completed")
}

func (s *server) handleCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	actor := inbox.ActorFrom(ctx)

	var signals struct {
		Reason string `json:"reason"`
	}
	if err := ds.ReadSignals("cancel-form", r, &signals); err != nil {
		s.sseError(w, r, err)
		return
	}

	_, err := s.ib.Cancel(ctx, id, actor, signals.Reason)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Item cancelled")
}

func (s *server) handleComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	actor := inbox.ActorFrom(ctx)

	var signals struct {
		Body string `json:"body"`
	}
	if err := ds.ReadSignals("comment-form", r, &signals); err != nil {
		s.sseError(w, r, err)
		return
	}

	_, err := s.ib.Comment(ctx, id, actor, signals.Body, nil)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	s.refreshDetailAndToast(w, r, id, "Comment added")
}

// refreshDetailAndToast re-renders the detail drawer, updates the queue row, and shows a toast.
func (s *server) refreshDetailAndToast(w http.ResponseWriter, r *http.Request, id string, msg string) {
	ctx := r.Context()
	actor := inbox.ActorFrom(ctx)

	item, err := s.ib.Get(ctx, id)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	data := detailData{
		Item:     item,
		Actor:    actor,
		BasePath: s.cfg.basePath,
	}
	if fn, ok := s.cfg.payloadRenderers[item.PayloadType()]; ok {
		if item.Proto.GetPayload() != nil {
			data.PayloadComponent = fn(item.PayloadType(), item.Proto.GetPayload().GetValue())
		}
	}
	assignee := inbox.TagValue(item, "assignee:")
	data.IsClaimant = assignee == actor

	sse := datastar.NewSSE(w, r)

	// Update drawer content
	ds.Send.Drawer(sse, detailDrawer(data))

	// Update the corresponding queue table row
	sse.PatchElementTempl(queueRow(item, s.cfg.basePath))

	// Show toast
	ds.Send.Toast(sse, ds.ToastSuccess, msg)
}

func (s *server) handleGenerateLink(w http.ResponseWriter, r *http.Request) {
	if s.cfg.signer == nil {
		s.sseError(w, r, fmt.Errorf("link generation not configured"))
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "id")

	item, err := s.ib.Get(ctx, id)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	// Use assignee as the token actor, or fall back to a generic client actor
	actor := inbox.TagValue(item, "assignee:")
	if actor == "" {
		actor = "client:" + id
	}

	token, _, err := s.cfg.signer.Sign(ctx, id, actor, inbox.ScopeRespond, s.cfg.linkExpiry)
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	link := s.cfg.linkBaseURL + "?token=" + token

	sse := datastar.NewSSE(w, r)
	ds.Send.Toast(sse, ds.ToastSuccess, link,
		ds.WithToastDuration(0),
		ds.WithToastPersistent(),
	)
}

func (s *server) sseError(w http.ResponseWriter, r *http.Request, err error) {
	sse := datastar.NewSSE(w, r)
	ds.Send.Toast(sse, ds.ToastError, err.Error())
}
