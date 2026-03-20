# Inbox Content Provider Architecture

**Date:** 2026-03-20
**Status:** Approved

## Problem

The inbox module currently couples item rendering and interaction into the core package via `ItemSchema` (proto with `DisplayField`, `FormField`, `Action`) and `WithPayloadRenderer`. This means the inbox has opinions about what items look like and how users interact with them. Any information or interaction should be renderable inside an inbox item, with the producing system controlling the full content.

## Design

Decouple the inbox from all rendering and content concerns. The inbox becomes a pure lifecycle/state container. Producers (internal Go modules) control what's shown and how users interact with items. The inbox provides lifecycle hooks so producers can react to inbox-driven transitions, and producers call the inbox API directly when their flows resolve.

### Package Structure

```
inbox/
├── inbox.go              — Inbox type, constructor
├── item.go               — Item, Event, Meta, Response
├── status.go             — Status constants
├── create.go             — Create
├── get.go                — Get, ListByTags, Search, etc.
├── lifecycle.go          — Claim, Release, Respond, Complete, Cancel, Expire
├── events.go             — AddEvent, Comment, Escalate, Reassign
├── tags.go               — Tag, Untag, HasTag, etc.
├── op.go                 — Op builder
├── hooks.go              — LifecycleHooks interface + registry (NEW)
├── dispatcher.go         — Dispatcher interface (unchanged)
├── actor.go              — context helpers (unchanged)
├── proto/inbox/v1/
│   ├── item.proto        — Item, Event (unchanged)
│   └── events.proto      — event types (unchanged)
│
├── ui/
│   ├── handler.go        — HTTP routes (slimmed down)
│   ├── config.go         — WithContentProvider, WithLayout, etc.
│   ├── provider.go       — ContentProvider interface (NEW)
│   ├── queue.go          — queue list view
│   ├── mywork.go         — my work view
│   ├── search.go         — search view
│   └── detail.go         — detail drawer (now just a shell)
│
├── schema/               — NEW separate package
│   ├── schema.go         — ItemSchema content provider implementation
│   ├── render.go         — templ components for form/display/actions
│   └── proto/            — schema.proto moves here
│
└── token/                — NEW separate package
    ├── token.go          — Claims, Signer, Verifier interfaces
    ├── handler.go        — /respond HTTP handler
    └── render.go         — client-facing response UI
```

### LifecycleHooks Interface (inbox core)

```go
// inbox/hooks.go

// LifecycleHooks lets producers react to inbox-driven state transitions.
// All methods are optional — embed DefaultHooks to stub them out.
type LifecycleHooks interface {
    OnClaim(ctx context.Context, itemID, actor string) error
    OnRelease(ctx context.Context, itemID, actor string) error
    OnCancel(ctx context.Context, itemID, actor, reason string) error
    OnComplete(ctx context.Context, itemID, actor string) error
}

// DefaultHooks is a no-op implementation.
type DefaultHooks struct{}
```

**Registration:** `inbox.WithLifecycleHooks(payloadType string, hooks LifecycleHooks) Option`

**Semantics:**
- Hooks are looked up by `item.PayloadType()` and called **after** the state transition succeeds (entity store write committed).
- Hook errors do not roll back the state transition. Same semantics as the existing Dispatcher.
- The error is returned to the caller who can decide what to do.

### ContentProvider Interface (inbox/ui)

```go
// inbox/ui/provider.go

type RenderContext struct {
    Item     inbox.Item
    Actor    string
    BasePath string // e.g., "/inbox" — for building lifecycle URLs
}

type ContentProvider interface {
    Render(ctx context.Context, rc RenderContext) templ.Component
}
```

**Registration:** `ui.WithContentProvider(payloadType string, provider ContentProvider) Option`

**Detail view rendering:**
1. Get item by ID.
2. Look up ContentProvider by `item.PayloadType()`.
3. If found: render inbox shell (header, status badge, event timeline) + `provider.Render()` in the content area.
4. If not found: render fallback (event timeline only, no content).

**The inbox shell owns:** item header (title, status, assignee), event/audit timeline, the "Cancel" button in inbox chrome.

**The provider owns:** everything in the content area — forms, details, action buttons, multi-step flows. Its rendered HTML can target inbox lifecycle endpoints directly via Datastar (e.g., `$$post('/inbox/items/{id}/complete')`).

### Schema Package (inbox/schema)

The existing `ItemSchema` proto and rendering becomes a standalone content provider.

```go
type Provider struct{}

func (p Provider) Render(ctx context.Context, rc ui.RenderContext) templ.Component {
    schema, _ := inbox.UnpackPayload[*schemapb.ItemSchema](rc.Item)
    // renders DisplayFields + FormFields + Actions as today
}
```

`schema.proto` moves from `proto/inbox/v1/` to `inbox/schema/proto/`. The inbox core no longer references `ItemSchema`.

### Token Package (inbox/token)

Presigned link / client-facing response handling becomes its own package.

```go
type Scope string

const (
    ScopeRespond Scope = "respond"
    ScopeView    Scope = "view"
)

type Claims struct {
    ItemID   string
    Actor    string
    Scope    Scope
    Exp      time.Time
    IssuedAt time.Time
}

type Signer interface {
    Sign(claims Claims) (string, error)
}

type Verifier interface {
    Verify(token string) (Claims, error)
}

// Handler serves the client-facing /respond endpoint.
type Handler struct {
    inbox    *inbox.Inbox
    verifier Verifier
    provider ui.ContentProvider
}
```

The token package is fully independent — any content provider can be used for the client-facing view.

### Data Flow

**Producer-driven completion (e.g., CS agent resolves an invoice dispute):**
1. Agent opens item → inbox shell renders header/timeline, `invoiceProvider.Render()` fills content area.
2. Agent picks resolution code, fills in details, clicks Submit.
3. Producer's HTTP handler processes the domain logic, then calls `ib.Complete(ctx, itemID, actor)`.
4. Inbox transitions to completed. No hooks needed — the producer drove it.

**Inbox-driven cancellation (agent clicks Cancel on inbox chrome):**
1. Agent clicks "Cancel" on the inbox shell.
2. Inbox calls `Cancel()` → state transitions → calls `invoiceHooks.OnCancel()`.
3. Invoice system cleans up its domain state.

### Wiring Example

```go
func main() {
    store := entitystore.New(...)
    ib := inbox.New(store, gen.ItemEntityType(),
        inbox.WithLifecycleHooks("myapp.v1.Invoice", invoiceHooks{}),
        inbox.WithDispatcher(myDispatcher),
    )

    uiHandler := ui.New(ib,
        ui.WithContentProvider("myapp.v1.Invoice", invoiceProvider{}),
        ui.WithContentProvider("inbox.v1.ItemSchema", schema.Provider{}),
        ui.WithLayout(myLayout),
        ui.WithBasePath("/inbox"),
        ui.WithActor(extractActor),
    )

    tokenHandler := token.NewHandler(ib, myVerifier, schema.Provider{})

    mux := chi.NewRouter()
    mux.Mount("/inbox", uiHandler)
    mux.Handle("/respond", tokenHandler)
}
```

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Content rendering model | Fully opaque — producer controls all HTML | CS portal needs diverse item types with different interactions |
| Communication mechanism | Datastar conventions ($$post to inbox endpoints) | Already in the stack, no iframe overhead, feels native |
| Producers | Internal Go modules only | Single monorepo, no need for HTML sanitization |
| Registration pattern | Explicit `WithContentProvider` / `WithLifecycleHooks` | Clear, discoverable, type-safe |
| Hook placement | Hooks on inbox core, rendering on UI | Hooks are domain concerns, rendering is presentation |
| Hook error semantics | Don't roll back state transitions | Same as Dispatcher — notification, not gating |
| ItemSchema | Separate package, ships as built-in provider | Handles 80% simple case, reference implementation |
| Presigned links | Own package | Orthogonal capability any provider might use |
