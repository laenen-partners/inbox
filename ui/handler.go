package ui

import (
	"context"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/inbox/gen/inbox/v1/inboxv1connect"
	"github.com/laenen-partners/tags"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Handler returns a mountable chi.Router for the inbox UI.
func Handler(client inboxv1connect.InboxServiceClient, opts ...Option) chi.Router {
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}

	s := &server{client: client, cfg: cfg}

	r := chi.NewRouter()
	r.Use(s.actorMiddleware)

	r.Get("/", s.handleQueue)
	r.Get("/mywork", s.handleMyWork)
	r.Get("/search", s.handleSearch)
	r.Get("/items/{id}", s.handleDetail)

	r.Get("/items/{id}/detail", s.handleDetailReload)
	r.Get("/items/{id}/row", s.handleRowReload)

	r.Post("/items/{id}/claim", s.handleClaim)
	r.Post("/items/{id}/release", s.handleRelease)
	r.Post("/items/{id}/close", s.handleClose)
	r.Post("/items/{id}/comment", s.handleComment)

	return r
}

type server struct {
	client inboxv1connect.InboxServiceClient
	cfg    *config
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
	_ = page.Render(r.Context(), w)
}

// identityToProto converts the identity from context into a proto Identity message.
func identityToProto(ctx context.Context) *inboxv1.Identity {
	id := identity.MustFromContext(ctx)
	return &inboxv1.Identity{
		TenantId:      id.TenantID(),
		WorkspaceId:   id.WorkspaceID(),
		PrincipalId:   id.PrincipalID(),
		PrincipalType: string(id.PrincipalType()),
		Roles:         id.Roles(),
	}
}

// fromProto converts a proto InboxItem into a domain inbox.Item.
func fromProto(pb *inboxv1.InboxItem) inbox.Item {
	var createdAt, updatedAt time.Time
	if pb.CreatedAt != nil {
		createdAt = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		updatedAt = pb.UpdatedAt.AsTime()
	}
	return inbox.Item{
		ID:        pb.Id,
		Proto:     pb.Data,
		Tags:      tags.FromStrings(pb.Tags),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

// fromProtoSlice converts a slice of proto InboxItem into domain items.
func fromProtoSlice(pbs []*inboxv1.InboxItem) []inbox.Item {
	items := make([]inbox.Item, len(pbs))
	for i, pb := range pbs {
		items[i] = fromProto(pb)
	}
	return items
}

// cursorToProto converts an optional time cursor to a proto Timestamp.
func cursorToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}
