package ui

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/laenen-partners/inbox"
)

type ctxKey string

const actorKey ctxKey = "inbox-ui-actor"

// Handler returns a mountable chi.Router for the inbox UI.
func Handler(ib *inbox.Inbox, opts ...Option) chi.Router {
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}

	s := &server{ib: ib, cfg: cfg}

	r := chi.NewRouter()
	r.Use(s.actorMiddleware)

	r.Get("/", s.handleQueue)
	r.Get("/mywork", s.handleMyWork)
	r.Get("/search", s.handleSearch)
	r.Get("/items/{id}", s.handleDetail)

	r.Post("/items/{id}/claim", s.handleClaim)
	r.Post("/items/{id}/release", s.handleRelease)
	r.Post("/items/{id}/respond", s.handleRespond)
	r.Post("/items/{id}/complete", s.handleComplete)
	r.Post("/items/{id}/cancel", s.handleCancel)
	r.Post("/items/{id}/comment", s.handleComment)

	return r
}

type server struct {
	ib  *inbox.Inbox
	cfg *config
}

func (s *server) actorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := s.cfg.actorFn(r)
		ctx := context.WithValue(r.Context(), actorKey, actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func actorFrom(ctx context.Context) string {
	if v, ok := ctx.Value(actorKey).(string); ok {
		return v
	}
	return "anonymous"
}

