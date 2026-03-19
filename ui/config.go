package ui

import (
	"net/http"

	"github.com/a-h/templ"
)

// FilterConfig defines a preset tag filter shown in the filter bar.
type FilterConfig struct {
	Label     string   // Display label (e.g. "Team")
	TagPrefix string   // Tag prefix to filter by (e.g. "team:")
	Options   []string // Available values (e.g. ["compliance", "ops"])
}

// PayloadRendererFunc renders a typed payload into a templ component.
// It receives the raw proto Any bytes and the payload type URL.
type PayloadRendererFunc func(payloadType string, data []byte) templ.Component

// Option configures the inbox UI handler.
type Option func(*config)

type config struct {
	actorFn          func(r *http.Request) string
	filters          []FilterConfig
	payloadRenderers map[string]PayloadRendererFunc
	basePath         string
}

func defaultConfig() *config {
	return &config{
		actorFn:          func(r *http.Request) string { return "anonymous" },
		payloadRenderers: make(map[string]PayloadRendererFunc),
	}
}

// WithActor sets the function that extracts the current actor from a request.
func WithActor(fn func(r *http.Request) string) Option {
	return func(c *config) { c.actorFn = fn }
}

// WithFilter adds a preset tag filter to the filter bar.
func WithFilter(f FilterConfig) Option {
	return func(c *config) { c.filters = append(c.filters, f) }
}

// WithPayloadRenderer registers a custom renderer for a specific payload type.
func WithPayloadRenderer(payloadType string, fn PayloadRendererFunc) Option {
	return func(c *config) { c.payloadRenderers[payloadType] = fn }
}

// WithBasePath sets the URL prefix for link generation.
func WithBasePath(path string) Option {
	return func(c *config) { c.basePath = path }
}
