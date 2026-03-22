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

// RichContentProvider extends ContentProvider with drawer size and context
// links. Implement this instead of ContentProvider to control drawer width
// and provide related links. The UI handler checks for this interface first
// via type assertion.
type RichContentProvider interface {
	ContentProvider
	RenderRich(ctx context.Context, rc RenderContext) RenderResult
}

// RenderResult is the return type of RichContentProvider.RenderRich.
type RenderResult struct {
	Content templ.Component
	Size    DrawerSize
	Links   []Link
}

// DrawerSize controls the width of the detail drawer.
type DrawerSize string

const (
	DrawerSizeDefault DrawerSize = ""
	DrawerSizeWide    DrawerSize = "wide"
)

// Link is a context link shown in the detail drawer.
type Link struct {
	Label string
	URL   string
}

// ContentOnly wraps a templ.Component into a RenderResult with default
// size and no links.
func ContentOnly(c templ.Component) RenderResult {
	return RenderResult{Content: c}
}

// WorkflowStatusProvider is an optional interface that ContentProviders
// can implement to show the parent workflow's status in the detail drawer.
// The UI handler checks for this via type assertion after calling
// Render/RenderRich.
type WorkflowStatusProvider interface {
	WorkflowStatus(ctx context.Context, item inbox.Item) (WorkflowState, error)
}

// WorkflowState describes the current state of the parent workflow.
type WorkflowState struct {
	Status string // "running", "completed", "failed", "waiting"
	Label  string // Human-readable label, e.g., "Workflow resumed"
}
