package ui

import (
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
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

// LayoutFunc wraps page content in an application layout.
// currentPath is the active route (e.g. "/", "/mywork", "/search").
// content is the page body to render inside the layout.
type LayoutFunc func(currentPath string, content templ.Component) templ.Component

// Option configures the inbox UI handler.
type Option func(*config)

type config struct {
	identityFn       func(r *http.Request) identity.Context
	filters          []FilterConfig
	payloadRenderers map[string]PayloadRendererFunc
	basePath         string
	layoutFn         LayoutFunc
	signer           inbox.Signer
	verifier         inbox.Verifier
	linkBaseURL      string        // base URL for presigned links (e.g. "https://app.example.com/respond")
	linkExpiry       time.Duration // how long presigned links are valid
}

func defaultConfig() *config {
	defaultID, _ := identity.New("default", "default", "anonymous", identity.PrincipalService, nil)
	return &config{
		identityFn:       func(r *http.Request) identity.Context { return defaultID },
		payloadRenderers: make(map[string]PayloadRendererFunc),
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

// WithPayloadRenderer registers a custom renderer for a specific payload type.
func WithPayloadRenderer(payloadType string, fn PayloadRendererFunc) Option {
	return func(c *config) { c.payloadRenderers[payloadType] = fn }
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

// WithSigner enables presigned link generation for inbox items.
// linkBaseURL is the public URL prefix for the client-facing endpoint
// (e.g. "https://app.example.com/respond"). The generated link appends ?token=<jwt>.
// expiry controls how long the links are valid.
func WithSigner(signer inbox.Signer, linkBaseURL string, expiry time.Duration) Option {
	return func(c *config) {
		c.signer = signer
		c.linkBaseURL = linkBaseURL
		c.linkExpiry = expiry
	}
}

// WithVerifier enables the client-facing /respond endpoint that accepts
// presigned JWT tokens. The verifier validates the token and extracts claims.
func WithVerifier(v inbox.Verifier) Option {
	return func(c *config) { c.verifier = v }
}
