package ui

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/pubsub"
)

// FilterConfig defines a preset tag filter shown in the filter bar.
type FilterConfig struct {
	Label     string   // Display label (e.g. "Team")
	TagPrefix string   // Tag prefix to filter by (e.g. "team:")
	Options   []string // Available values (e.g. ["compliance", "ops"])
}

// LayoutFunc wraps page content in an application layout.
// currentPath is the active route (e.g. "/", "/mywork", "/search").
// content is the page body to render inside the layout.
type LayoutFunc func(currentPath string, content templ.Component) templ.Component

// Option configures the inbox UI handler.
type Option func(*config)

type config struct {
	identityFn       func(r *http.Request) identity.Context
	filters          []FilterConfig
	contentProviders map[string]ContentProvider
	basePath         string
	layoutFn         LayoutFunc
	bus              *pubsub.Bus
}

func defaultConfig() *config {
	defaultID, _ := identity.New("default", "default", "anonymous", identity.PrincipalService, nil)
	return &config{
		identityFn:       func(r *http.Request) identity.Context { return defaultID },
		contentProviders: make(map[string]ContentProvider),
	}
}

// WithIdentity sets the function that extracts the caller's identity from a request.
func WithIdentity(fn func(r *http.Request) identity.Context) Option {
	return func(c *config) { c.identityFn = fn }
}

// WithFilter adds a preset tag filter to the filter bar.
func WithFilter(f FilterConfig) Option {
	return func(c *config) { c.filters = append(c.filters, f) }
}

// WithContentProvider registers a content provider for a specific payload type.
// When an item's PayloadType matches, the provider controls the content area
// of the detail drawer. Shell elements (header, meta, timeline, comment, shell
// actions) remain owned by the inbox UI.
func WithContentProvider(payloadType string, provider ContentProvider) Option {
	return func(c *config) { c.contentProviders[payloadType] = provider }
}

// WithBasePath sets the URL prefix for link generation.
func WithBasePath(path string) Option {
	return func(c *config) { c.basePath = path }
}

// WithLayout sets a custom layout wrapper for page rendering.
// When set, replaces the default layout (Base + navbar tabs).
func WithLayout(fn LayoutFunc) Option {
	return func(c *config) { c.layoutFn = fn }
}

// WithBus sets the pub/sub bus for publishing change notifications.
// When set, action handlers publish notifications so the stream relay
// can push stale signals to connected clients.
func WithBus(bus *pubsub.Bus) Option {
	return func(c *config) { c.bus = bus }
}
