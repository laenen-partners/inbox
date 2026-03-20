package ui

import (
	"context"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
)

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
		id := s.cfg.identityFn(r)
		ctx := identity.WithContext(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func actorStr(ctx context.Context) string {
	id := identity.MustFromContext(ctx)
	return string(id.PrincipalType()) + ":" + id.PrincipalID()
}

// renderPage wraps content in the configured layout and renders to the response.
func (s *server) renderPage(w http.ResponseWriter, r *http.Request, currentPath string, content templ.Component) {
	var page templ.Component
	if s.cfg.layoutFn != nil {
		page = s.cfg.layoutFn(currentPath, content)
	} else {
		page = defaultLayout(s.cfg, currentPath, content)
	}
	page.Render(r.Context(), w)
}
