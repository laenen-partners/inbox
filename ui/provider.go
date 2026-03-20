package ui

import (
	"context"

	"github.com/a-h/templ"
	"github.com/laenen-partners/inbox"
)

// RenderContext carries everything a content provider needs to render.
type RenderContext struct {
	Item     inbox.Item
	Actor    string
	BasePath string
}

// ContentProvider renders the detail view content for a specific payload type.
type ContentProvider interface {
	Render(ctx context.Context, rc RenderContext) templ.Component
}
