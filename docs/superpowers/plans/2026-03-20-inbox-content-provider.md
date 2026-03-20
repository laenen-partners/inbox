# Inbox Content Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decouple the inbox module from rendering/content concerns so any producer can control what's displayed and how users interact with inbox items.

**Architecture:** LifecycleHooks on inbox core for domain reactions, ContentProvider on inbox/ui for rendering delegation, schema and token packages extracted as independent modules. Producers register at the composition root.

**Tech Stack:** Go, protobuf, templ, Datastar, chi, entity store, testcontainers

**Spec:** `docs/superpowers/specs/2026-03-20-inbox-content-provider-design.md`

---

### Task 1: Add LifecycleHooks to inbox core

Additive change — no existing behavior changes. Introduces the hook interface, registration, and wiring into lifecycle methods.

**Files:**
- Create: `hooks.go`
- Modify: `inbox.go:31-34` (add hooks map to Inbox struct)
- Modify: `options.go` (add WithLifecycleHooks)
- Modify: `lifecycle.go:16-20,23-27,75-86,89-100,104-115,164-188` (call hooks after transitions)
- Modify: `op.go:185-249` (call hooks after TransitionTo in Apply)
- Test: `inbox_test.go`

- [ ] **Step 1: Write the failing test for lifecycle hooks**

Add a test to `inbox_test.go` that registers a `LifecycleHooks` implementation and verifies hooks fire on Claim, Release, Complete, Cancel, and Expire.

```go
func TestLifecycleHooks(t *testing.T) {
	// Build inbox with hooks registered for the payload type we'll use.
	// Cannot use sharedInbox() because we need WithLifecycleHooks option.
	ctx := context.Background()
	es := sharedEntityStore(t) // extract the entity store setup from sharedInbox
	recorder := &hookRecorder{}
	ib := inbox.New(es,
		inbox.WithLifecycleHooks("inbox.v1.ItemSchema", recorder),
	)

	actorCtx := ctxWithActor("hook-tester", identity.PrincipalUser)

	// Create an item with a known payload type
	item, err := ib.Create(actorCtx, inbox.Meta{
		Title: "Hook Test Item",
		Payload: &inboxv1.ItemSchema{
			Display: []*inboxv1.DisplayField{{Label: "Test", Value: "Value"}},
		},
	})
	require.NoError(t, err)

	// Claim should trigger OnClaim
	_, err = ib.Claim(actorCtx, item.ID)
	require.NoError(t, err)
	require.Equal(t, 1, recorder.claimCount)
	require.Equal(t, item.ID, recorder.lastItemID)

	// Release should trigger OnRelease
	_, err = ib.Release(actorCtx, item.ID)
	require.NoError(t, err)
	require.Equal(t, 1, recorder.releaseCount)

	// Complete should trigger OnComplete
	_, err = ib.Complete(actorCtx, item.ID)
	require.NoError(t, err)
	require.Equal(t, 1, recorder.completeCount)
}
```

**Note:** Extract a `sharedEntityStore(t) *entitystore.EntityStore` helper from `sharedInbox` so tests can create an inbox with custom options. The existing `sharedInbox` calls `sharedEntityStore` + `inbox.New(es)`. The existing helper `ctxWithActor(principalID, principalType)` is already available in the test file — do NOT use `withActor` (which doesn't exist).

```go

type hookRecorder struct {
	inbox.DefaultHooks
	claimCount    int
	releaseCount  int
	completeCount int
	cancelCount   int
	expireCount   int
	lastItemID    string
}

func (h *hookRecorder) OnClaim(_ context.Context, itemID, _ string) error {
	h.claimCount++
	h.lastItemID = itemID
	return nil
}
func (h *hookRecorder) OnRelease(_ context.Context, itemID, _ string) error {
	h.releaseCount++
	h.lastItemID = itemID
	return nil
}
func (h *hookRecorder) OnComplete(_ context.Context, itemID, _ string) error {
	h.completeCount++
	h.lastItemID = itemID
	return nil
}
func (h *hookRecorder) OnCancel(_ context.Context, itemID, _, _ string) error {
	h.cancelCount++
	h.lastItemID = itemID
	return nil
}
func (h *hookRecorder) OnExpire(_ context.Context, itemID string) error {
	h.expireCount++
	h.lastItemID = itemID
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go build ./...`
Expected: compilation errors — `LifecycleHooks`, `DefaultHooks`, `WithLifecycleHooks` don't exist yet.

- [ ] **Step 3: Create hooks.go with interface and DefaultHooks**

Create `hooks.go`:

```go
package inbox

import "context"

// LifecycleHooks lets producers react to inbox-driven state transitions.
// All methods are optional — embed DefaultHooks to stub them out.
type LifecycleHooks interface {
	OnClaim(ctx context.Context, itemID, actor string) error
	OnRelease(ctx context.Context, itemID, actor string) error
	OnCancel(ctx context.Context, itemID, actor, reason string) error
	OnComplete(ctx context.Context, itemID, actor string) error
	OnExpire(ctx context.Context, itemID string) error
}

// DefaultHooks is a no-op implementation. Embed it to only override
// the hooks you need.
type DefaultHooks struct{}

func (DefaultHooks) OnClaim(context.Context, string, string) error          { return nil }
func (DefaultHooks) OnRelease(context.Context, string, string) error        { return nil }
func (DefaultHooks) OnCancel(context.Context, string, string, string) error { return nil }
func (DefaultHooks) OnComplete(context.Context, string, string) error       { return nil }
func (DefaultHooks) OnExpire(context.Context, string) error                 { return nil }
```

- [ ] **Step 4: Add hooks map to Inbox struct and WithLifecycleHooks option**

In `inbox.go`, add `hooks map[string]LifecycleHooks` to the Inbox struct and initialize it in `New()`.

In `options.go`, add:

```go
// WithLifecycleHooks registers hooks for items with the given payload type.
// Hooks fire after state transitions succeed. Hook errors are returned
// to the caller but do not roll back the transition.
func WithLifecycleHooks(payloadType string, hooks LifecycleHooks) Option {
	return func(ib *Inbox) {
		if ib.hooks == nil {
			ib.hooks = make(map[string]LifecycleHooks)
		}
		ib.hooks[payloadType] = hooks
	}
}
```

- [ ] **Step 5: Add fireHook helper and wire into lifecycle.go**

Add a helper method to `hooks.go`:

```go
// fireHook looks up hooks by payload type and calls the given function.
// Returns nil if no hooks are registered for this payload type.
func (ib *Inbox) fireHook(item Item, fn func(LifecycleHooks) error) error {
	if ib.hooks == nil {
		return nil
	}
	h, ok := ib.hooks[item.PayloadType()]
	if !ok {
		return nil
	}
	return fn(h)
}
```

Wire hooks into `lifecycle.go`:

- After `Claim` succeeds (after `transition()` returns), call:
  `ib.fireHook(item, func(h LifecycleHooks) error { return h.OnClaim(ctx, itemID, actor) })`

- Same pattern for `Release` (OnRelease), `Complete` (OnComplete), `Cancel` (OnCancel), `Expire` (OnExpire).

Each lifecycle method becomes:
```go
func (ib *Inbox) Claim(ctx context.Context, itemID string) (Item, error) {
	actor := actorFromCtx(ctx)
	item, err := ib.transition(ctx, itemID, StatusOpen, StatusClaimed,
		newProtoEvent(actor, &inboxv1.ItemClaimed{ClaimedBy: actor}))
	if err != nil {
		return Item{}, err
	}
	if hookErr := ib.fireHook(item, func(h LifecycleHooks) error {
		return h.OnClaim(ctx, itemID, actor)
	}); hookErr != nil {
		return item, hookErr
	}
	return item, nil
}
```

- [ ] **Step 6: Wire hooks into op.go Apply()**

In `op.go`, after the entity store write succeeds and after the dispatcher fires, add hook dispatch based on `op.newStatus`:

Use the same `fireHook` helper for consistency. Hook errors are returned to the caller (same as lifecycle methods):

```go
// Fire lifecycle hooks if status changed.
if op.newStatus != "" {
	var hookErr error
	switch op.newStatus {
	case StatusCompleted:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error {
			return h.OnComplete(op.ctx, item.ID, op.actor)
		})
	case StatusCancelled:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error {
			return h.OnCancel(op.ctx, item.ID, op.actor, "")
		})
	case StatusExpired:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error {
			return h.OnExpire(op.ctx, item.ID)
		})
	case StatusClaimed:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error {
			return h.OnClaim(op.ctx, item.ID, op.actor)
		})
	case StatusOpen:
		hookErr = op.ib.fireHook(item, func(h LifecycleHooks) error {
			return h.OnRelease(op.ctx, item.ID, op.actor)
		})
	}
	if hookErr != nil {
		return item, hookErr
	}
}
```

- [ ] **Step 7: Run tests to verify hooks fire**

Run: `go test -v -count=1 -timeout 120s -run TestLifecycleHooks ./...`
Expected: PASS — all hook counts match expectations.

- [ ] **Step 8: Run full test suite**

Run: `go test -v -count=1 -timeout 120s ./...`
Expected: All existing tests still pass.

- [ ] **Step 9: Commit**

```bash
git add hooks.go inbox.go options.go lifecycle.go op.go inbox_test.go
git commit -m "feat: add LifecycleHooks for producer-side transition reactions"
```

---

### Task 2: Add ContentProvider interface to inbox/ui

Additive change — introduces the provider interface and refactors the detail view to delegate rendering. Falls back to existing rendering when no provider is registered.

**Files:**
- Create: `ui/provider.go`
- Modify: `ui/config.go:31-49` (add contentProviders map)
- Modify: `ui/detail.go:14-64` (delegate to provider)
- Modify: `ui/detail.templ:10-100` (split into shell + content slot)
- Modify: `ui/actions.go:128-167` (refactor refreshDetailAndToast)
- Modify: `ui/actions.templ:8-41` (split shell actions from provider actions)

- [ ] **Step 1: Create provider.go with ContentProvider interface**

Create `ui/provider.go`:

```go
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
```

- [ ] **Step 2: Add WithContentProvider option and contentProviders map**

In `ui/config.go`, add `contentProviders map[string]ContentProvider` to the `config` struct. Initialize it in `defaultConfig()`. Add option:

```go
// WithContentProvider registers a content provider for items with the given payload type.
func WithContentProvider(payloadType string, provider ContentProvider) Option {
	return func(c *config) { c.contentProviders[payloadType] = provider }
}
```

- [ ] **Step 3: Refactor detailData struct**

In `ui/detail.go`, replace the current `detailData` struct with:

```go
type detailData struct {
	Item     inbox.Item
	Actor    string
	IsClaimant bool
	BasePath string
	Content  templ.Component // rendered by ContentProvider (nil if no provider)

	// Legacy fields — used during migration, removed in Task 5
	PayloadComponent templ.Component
	Schema           *inboxv1.ItemSchema
	CanLink          bool
}
```

- [ ] **Step 4: Refactor handleDetail to use ContentProvider**

In `ui/detail.go`, modify `handleDetail` to try ContentProvider first, then fall back to legacy rendering:

```go
func (s *server) handleDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	actor := actorStr(ctx)

	item, err := s.ib.Get(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	data := detailData{
		Item:     item,
		Actor:    actor,
		BasePath: s.cfg.basePath,
	}

	// Try ContentProvider first
	if provider, ok := s.cfg.contentProviders[item.PayloadType()]; ok {
		rc := RenderContext{Item: item, Actor: actor, BasePath: s.cfg.basePath}
		data.Content = provider.Render(ctx, rc)
	} else {
		// Legacy fallback — schema + custom renderer
		if item.Proto.GetPayload() != nil {
			data.Schema = tryParseSchema(item.PayloadType(), item.Proto.GetPayload().GetValue())
		}
		data.CanLink = s.cfg.signer != nil && data.Schema != nil && data.Schema.ClientCompletable
		if data.Schema == nil {
			if fn, ok := s.cfg.payloadRenderers[item.PayloadType()]; ok {
				if item.Proto.GetPayload() != nil {
					data.PayloadComponent = fn(item.PayloadType(), item.Proto.GetPayload().GetValue())
				}
			}
		}
	}

	assignee := inbox.TagValue(item, "assignee:")
	data.IsClaimant = assignee == actor

	sse := datastar.NewSSE(w, r)
	ds.Send.Drawer(sse, detailDrawer(data))
}
```

- [ ] **Step 5: Refactor detail.templ to support Content slot**

Modify `ui/detail.templ` — add a `Content` branch before the legacy schema/payload rendering:

```
// Payload / Content
if data.Content != nil {
    <div class="py-4">
        @data.Content
    </div>
} else if data.Schema != nil || data.PayloadComponent != nil || data.Item.Proto.GetPayload() != nil {
    // legacy rendering (unchanged)
}
```

- [ ] **Step 6: Split action buttons — shell vs provider**

Modify `ui/actions.templ` — when a ContentProvider is used (`data.Content != nil`), only show shell actions (Claim, Release, Cancel). Respond and Complete move to the provider's rendered content.

Update `actionButtons` in `actions.templ`:

```
templ actionButtons(data detailData) {
    <div class="flex flex-wrap gap-1.5">
        switch data.Item.Status() {
        case "open":
            @button.Button(button.Props{
                Variant: button.VariantNeutral,
                Size:    button.SizeSm,
                OnClick: ds.Post(data.BasePath + "/items/" + data.Item.ID + "/claim"),
            }) {
                Claim
            }
            @cancelButton(data)
        case "claimed":
            if data.IsClaimant {
                if data.Content == nil {
                    // Legacy: show respond + complete in shell
                    @respondButton(data)
                    @button.Button(button.Props{
                        Variant: button.VariantSuccess,
                        Size:    button.SizeSm,
                        OnClick: ds.Post(data.BasePath + "/items/" + data.Item.ID + "/complete"),
                    }) {
                        Complete
                    }
                }
                @button.Button(button.Props{
                    Variant: button.VariantGhost,
                    Size:    button.SizeSm,
                    OnClick: ds.Post(data.BasePath + "/items/" + data.Item.ID + "/release"),
                }) {
                    Release
                }
                @cancelButton(data)
            }
        }
    </div>
}
```

- [ ] **Step 7: Refactor refreshDetailAndToast**

In `ui/actions.go`, update `refreshDetailAndToast` to use the same ContentProvider logic as `handleDetail` (DRY it up by extracting a `buildDetailData` helper):

```go
func (s *server) buildDetailData(ctx context.Context, item inbox.Item, actor string) detailData {
	data := detailData{
		Item:     item,
		Actor:    actor,
		BasePath: s.cfg.basePath,
	}
	if provider, ok := s.cfg.contentProviders[item.PayloadType()]; ok {
		rc := RenderContext{Item: item, Actor: actor, BasePath: s.cfg.basePath}
		data.Content = provider.Render(ctx, rc)
	} else {
		if item.Proto.GetPayload() != nil {
			data.Schema = tryParseSchema(item.PayloadType(), item.Proto.GetPayload().GetValue())
		}
		data.CanLink = s.cfg.signer != nil && data.Schema != nil && data.Schema.ClientCompletable
		if data.Schema == nil {
			if fn, ok := s.cfg.payloadRenderers[item.PayloadType()]; ok {
				if item.Proto.GetPayload() != nil {
					data.PayloadComponent = fn(item.PayloadType(), item.Proto.GetPayload().GetValue())
				}
			}
		}
	}
	assignee := inbox.TagValue(item, "assignee:")
	data.IsClaimant = assignee == actor
	return data
}
```

Use this in both `handleDetail` and `refreshDetailAndToast`.

- [ ] **Step 8: Run templ generate**

Run: `templ generate`
Expected: templ files compile without errors.

- [ ] **Step 9: Build and run tests**

Run: `go build ./... && go test -v -count=1 -timeout 120s ./...`
Expected: All tests pass. Existing behavior unchanged (no providers registered yet).

- [ ] **Step 10: Commit**

```bash
git add ui/provider.go ui/config.go ui/detail.go ui/detail.templ ui/detail_templ.go ui/actions.go ui/actions.templ ui/actions_templ.go
git commit -m "feat(ui): add ContentProvider interface with legacy fallback"
```

---

### Task 3: Extract inbox/schema package

Moves `ItemSchema` proto, rendering, and helpers into a standalone package that implements `ContentProvider`.

**Files:**
- Create: `schema/provider.go`
- Create: `schema/render.templ` (moved from `ui/schema.templ`)
- Create: `schema/helpers.go` (moved from `ui/schema.go`)
- Create: `schema/proto/inbox/v1/schema.proto` (moved from `proto/inbox/v1/schema.proto`)
- Modify: `buf.gen.yaml` or `buf.yaml` (if proto path changes need config updates)
- Modify: `cmd/inboxui/main.go` (register schema provider)

- [ ] **Step 1: Create schema/proto directory and move schema.proto**

```bash
mkdir -p schema/proto/schema/v1
```

Copy `proto/inbox/v1/schema.proto` to `schema/proto/schema/v1/schema.proto`. **Critical:** Change the proto package from `inbox.v1` to `schema.v1` and update `go_package` to `github.com/laenen-partners/inbox/schema/gen/schema/v1;schemav1`. This avoids protobuf registration panics — two Go packages cannot register messages in the same proto package `inbox.v1`.

This means the payload type string for schema items changes from `"inbox.v1.ItemSchema"` to `"schema.v1.ItemSchema"`. Update `TryParse` and all seed data / test fixtures that create items with `ItemSchema` payloads. The `PayloadType()` is derived from `proto.MessageName()`, so existing items created before this migration will still have the old type — add a check for both `"inbox.v1.ItemSchema"` and `"schema.v1.ItemSchema"` in `TryParse` for backward compatibility.

Remove the original `proto/inbox/v1/schema.proto` and its generated code `gen/inbox/v1/schema.pb.go`.

- [ ] **Step 2: Update buf config for schema proto**

Create `schema/buf.yaml`:

```yaml
version: v2
modules:
  - path: proto
```

Create `schema/buf.gen.yaml`:

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
```

Run from `schema/`:

```bash
cd schema && buf generate && cd ..
```

Verify generated code lands in `schema/gen/schema/v1/schema.pb.go`.

Also update the root `Taskfile` or build commands to include `cd schema && buf generate` in the generate step.

- [ ] **Step 3: Create schema/helpers.go**

Move `tryParseSchema` and `buildSchemaSignals` from `ui/schema.go` to `schema/helpers.go`. Update imports to use the new generated schema package.

```go
package schema

import (
	"encoding/json"

	schemav1 "github.com/laenen-partners/inbox/schema/gen/schema/v1"
	"google.golang.org/protobuf/proto"
)

// TryParse attempts to unmarshal the payload as an ItemSchema.
// Returns nil if the payload type doesn't match or parsing fails.
// Accepts both "schema.v1.ItemSchema" (current) and "inbox.v1.ItemSchema"
// (legacy, for items created before the proto package rename).
func TryParse(payloadType string, data []byte) *schemav1.ItemSchema {
	if payloadType != "schema.v1.ItemSchema" && payloadType != "inbox.v1.ItemSchema" {
		return nil
	}
	var s schemav1.ItemSchema
	if err := proto.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

// BuildSignals builds a JSON string for Datastar data-signals
// from the schema's form fields, namespaced under "schema".
func BuildSignals(schema *schemav1.ItemSchema) string {
	fields := make(map[string]interface{})
	for _, f := range schema.Fields {
		if f.Type == "checkbox" {
			fields[f.Name] = f.DefaultValue == "true"
		} else {
			fields[f.Name] = f.DefaultValue
		}
	}
	wrapper := map[string]interface{}{"schema": fields}
	b, _ := json.Marshal(wrapper)
	return string(b)
}
```

- [ ] **Step 4: Create schema/render.templ**

Move the contents of `ui/schema.templ` to `schema/render.templ`. Update:
- Package declaration to `package schema`
- Imports to use `schemav1` from the new generated path
- Function names: `schemaPayload` → `Payload`, `schemaField` → `Field`, `schemaActionButton` → `ActionButton` (exported)
- `buildSchemaSignals` → `BuildSignals` (from helpers.go)
- `buttonVariant` helper moves here too

- [ ] **Step 5: Create schema/provider.go**

```go
package schema

import (
	"context"

	"github.com/a-h/templ"
	"github.com/laenen-partners/inbox"
	"github.com/laenen-partners/inbox/ui"
)

// Provider implements ui.ContentProvider for ItemSchema payloads.
type Provider struct{}

// Render parses the item's payload as an ItemSchema and renders
// the display fields, form fields, and action buttons.
func (p Provider) Render(ctx context.Context, rc ui.RenderContext) templ.Component {
	if rc.Item.Proto.GetPayload() == nil {
		return templ.NopComponent
	}
	schema := TryParse(rc.Item.PayloadType(), rc.Item.Proto.GetPayload().GetValue())
	if schema == nil {
		return templ.NopComponent
	}
	return Payload(schema, rc.Item.ID, rc.BasePath)
}
```

- [ ] **Step 6: Run templ generate in schema package**

Run: `templ generate && go build ./...`
Expected: compiles successfully.

- [ ] **Step 7: Update cmd/inboxui/main.go to register schema provider**

Add import for `schema` package. Register it:

```go
inboxui.WithContentProvider("schema.v1.ItemSchema", schema.Provider{}),
```

- [ ] **Step 8: Remove ui/schema.go and ui/schema.templ**

Delete `ui/schema.go` and `ui/schema.templ` (and `ui/schema_templ.go`). Since the schema `ContentProvider` is now registered in `main.go`, the ContentProvider path in `handleDetail` / `buildDetailData` will handle all `ItemSchema` items — the legacy `tryParseSchema` / `schemaPayload` calls are dead code.

**Also update the legacy fallback code** in `ui/detail.go` (`buildDetailData`): remove the `tryParseSchema` call and the `data.Schema` / `data.CanLink` assignments from the `else` branch. The legacy fallback should only handle `payloadRenderers`. This avoids compilation errors from the deleted `tryParseSchema` function.

Similarly, update `ui/detail.templ` to remove references to `data.Schema` and `schemaPayload` — these templ functions no longer exist in the `ui` package.

- [ ] **Step 9: Build and test**

Run: `go build ./... && go test -v -count=1 -timeout 120s ./...`
Expected: All tests pass. UI renders schema items via the new provider.

- [ ] **Step 10: Commit**

```bash
git add schema/ cmd/inboxui/main.go
git rm ui/schema.go ui/schema.templ ui/schema_templ.go proto/inbox/v1/schema.proto gen/inbox/v1/schema.pb.go
git commit -m "refactor: extract inbox/schema as standalone ContentProvider package"
```

---

### Task 4: Extract inbox/token package

Moves Signer/Verifier interfaces, claims types, and the client-facing `/respond` handler into an independent package.

**Files:**
- Create: `token/token.go`
- Create: `token/handler.go`
- Create: `token/render.templ` (moved from `ui/client.templ`)
- Modify: `ui/handler.go:37-38` (remove /respond routes)
- Modify: `cmd/inboxui/main.go` (mount token handler separately)
- Modify: `cmd/inboxui/tokens.go` (implement new token.Signer/Verifier)
- Delete: `token.go` (from inbox core)
- Delete: `ui/client.go`
- Delete: `ui/client.templ`

- [ ] **Step 1: Create token/token.go**

```go
package token

import (
	"context"
	"time"
)

// Scope controls what a token holder can do.
type Scope string

const (
	ScopeRespond Scope = "respond"
	ScopeView    Scope = "view"
)

// Claims carries the verified claims from a presigned token.
type Claims struct {
	ItemID   string
	Actor    string
	Scope    Scope
	Exp      time.Time
	IssuedAt time.Time
}

// Signer creates presigned tokens for inbox items.
type Signer interface {
	Sign(ctx context.Context, itemID, actor string, scope Scope, expiry time.Duration) (token string, exp time.Time, err error)
}

// Verifier validates presigned tokens and returns the claims.
type Verifier interface {
	Verify(ctx context.Context, tokenStr string) (*Claims, error)
}
```

- [ ] **Step 2: Create token/handler.go**

Move the logic from `ui/client.go` (`handleClientRespond` and `handleClientRespondSubmit`) into `token/handler.go`. The handler takes an `*inbox.Inbox`, a `Verifier`, and a `ui.ContentProvider`.

```go
package token

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/laenen-partners/inbox"
	"github.com/laenen-partners/inbox/ui"
)

// Handler serves the client-facing presigned link endpoints.
type Handler struct {
	inbox    *inbox.Inbox
	verifier Verifier
	provider ui.ContentProvider
}

// NewHandler creates a handler for client-facing presigned links.
func NewHandler(ib *inbox.Inbox, v Verifier, provider ui.ContentProvider) *Handler {
	return &Handler{inbox: ib, verifier: v, provider: provider}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodPost:
		h.handlePost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
```

Move the GET (verify token, render form) and POST (verify token, respond, complete) logic from `ui/client.go`, adapting to use `token.Claims` instead of `inbox.TokenClaims`.

- [ ] **Step 3: Create token/render.templ**

Move `ui/client.templ` to `token/render.templ`. Update package to `package token`. Update imports.

**Important:** The standalone page structure (Base layout, minimal header, footer with security warning) stays in the token package — this is the page chrome for client-facing links. The `ContentProvider.Render()` is used only for the **form portion** inside the page body, not for the full page. The standalone page templ function calls `provider.Render(ctx, rc)` to get the form content, then wraps it in the standalone page layout. This way a drawer-optimized provider still works in the standalone context because it only renders the content area, not the surrounding shell.

- [ ] **Step 4: Update cmd/inboxui/tokens.go**

Update `HMACTokens` to implement `token.Signer` and `token.Verifier` (using `token.Scope` and `token.Claims` instead of `inbox.Signer`/`inbox.Verifier` and `inbox.TokenClaims`).

- [ ] **Step 5: Update cmd/inboxui/main.go**

Mount the token handler separately:

```go
import inboxtoken "github.com/laenen-partners/inbox/token"

// After the inbox mount:
tokenHandler := inboxtoken.NewHandler(ib, tokens, schema.Provider{})
r.Handle("/respond", tokenHandler)
```

Remove `inboxui.WithSigner(...)` and `inboxui.WithVerifier(...)` from the inbox UI handler options.

- [ ] **Step 6: Remove /respond routes from ui/handler.go**

In `ui/handler.go`, delete lines 37-38:
```go
r.Get("/respond", s.handleClientRespond)
r.Post("/respond", s.handleClientRespondSubmit)
```

- [ ] **Step 7: Delete old files**

Delete:
- `token.go` (inbox core — replaced by `token/token.go`)
- `ui/client.go` (moved to `token/handler.go`)
- `ui/client.templ` (moved to `token/render.templ`)

- [ ] **Step 8: Run templ generate, build, and test**

Run: `templ generate && go build ./... && go test -v -count=1 -timeout 120s ./...`
Expected: All tests pass. `/respond` endpoint works via the new token handler.

- [ ] **Step 9: Commit**

```bash
git add token/ cmd/inboxui/main.go cmd/inboxui/tokens.go ui/handler.go
git rm token.go ui/client.go ui/client.templ ui/client_templ.go
git commit -m "refactor: extract inbox/token as independent presigned link package"
```

---

### Task 5: Clean up legacy rendering code

Remove the old rendering paths now that ContentProvider and schema package handle everything.

**Files:**
- Modify: `ui/config.go` (remove WithPayloadRenderer, WithSigner, WithVerifier, legacy fields)
- Modify: `ui/detail.go` (remove legacy detailData fields)
- Modify: `ui/detail.templ` (remove legacy schema/payload branches)
- Modify: `ui/actions.go` (remove handleGenerateLink, clean up refreshDetailAndToast)
- Modify: `ui/actions.templ` (remove legacy respond/complete in shell)
- Modify: `ui/handler.go` (remove /link route)
- Delete: `ui/schema.go` (if not already deleted in Task 3)

- [ ] **Step 1: Remove legacy fields from config.go**

Remove from `config` struct:
- `payloadRenderers map[string]PayloadRendererFunc`
- `signer inbox.Signer`
- `verifier inbox.Verifier`
- `linkBaseURL string`
- `linkExpiry time.Duration`

Remove:
- `PayloadRendererFunc` type
- `WithPayloadRenderer` function
- `WithSigner` function
- `WithVerifier` function

Remove `payloadRenderers` initialization from `defaultConfig()`.

Remove `inbox` import if no longer needed.

- [ ] **Step 2: Simplify detailData struct**

In `ui/detail.go`, remove legacy fields:

```go
type detailData struct {
	Item       inbox.Item
	Actor      string
	IsClaimant bool
	BasePath   string
	Content    templ.Component
}
```

- [ ] **Step 3: Remove legacy rendering from handleDetail / buildDetailData**

Remove the schema/payloadRenderer fallback paths. Only use ContentProvider:

```go
func (s *server) buildDetailData(ctx context.Context, item inbox.Item, actor string) detailData {
	data := detailData{
		Item:     item,
		Actor:    actor,
		BasePath: s.cfg.basePath,
	}
	if provider, ok := s.cfg.contentProviders[item.PayloadType()]; ok {
		rc := RenderContext{Item: item, Actor: actor, BasePath: s.cfg.basePath}
		data.Content = provider.Render(ctx, rc)
	}
	assignee := inbox.TagValue(item, "assignee:")
	data.IsClaimant = assignee == actor
	return data
}
```

- [ ] **Step 4: Clean up detail.templ**

Remove the legacy `Schema`/`PayloadComponent`/`CanLink` branches. Content area becomes:

```
if data.Content != nil {
    <div class="py-4">
        @data.Content
    </div>
}
```

Remove the `sendLinkButton` from the header (link generation moves to the schema provider or token package).

- [ ] **Step 5: Clean up actions.templ**

When a ContentProvider is registered (`data.Content != nil`), the shell shows only Release + Cancel. When no provider is registered (`data.Content == nil`), the shell keeps Respond + Complete + Release + Cancel as a fallback so items without a provider are still actionable:

```
case "claimed":
    if data.IsClaimant {
        if data.Content == nil {
            // Fallback: no provider registered, show all actions in shell
            @respondButton(data)
            @button.Button(button.Props{
                Variant: button.VariantSuccess,
                Size:    button.SizeSm,
                OnClick: ds.Post(data.BasePath + "/items/" + data.Item.ID + "/complete"),
            }) {
                Complete
            }
        }
        @button.Button(button.Props{
            Variant: button.VariantGhost,
            Size:    button.SizeSm,
            OnClick: ds.Post(data.BasePath + "/items/" + data.Item.ID + "/release"),
        }) {
            Release
        }
        @cancelButton(data)
    }
```

Keep `respondButton` for the fallback path. Remove `sendLinkButton` (link generation moves to the schema provider or token package).

- [ ] **Step 6: Remove handleGenerateLink from actions.go**

Delete `handleGenerateLink` function only. **Keep `handleRespond`** — it is a core inbox lifecycle endpoint (`ib.Respond()`) that providers' rendered action buttons POST to. It is NOT a legacy rendering concern.

- [ ] **Step 7: Remove /link route from handler.go**

In `ui/handler.go`, remove only the link route:
```go
r.Post("/items/{id}/link", s.handleGenerateLink)
```

**Keep** `r.Post("/items/{id}/respond", s.handleRespond)` — providers need this endpoint.

- [ ] **Step 8: Run templ generate, build, vet**

Run: `templ generate && go build ./... && go vet ./...`
Expected: compiles, no vet warnings.

- [ ] **Step 9: Run full test suite**

Run: `go test -v -count=1 -timeout 120s ./...`
Expected: All tests pass.

- [ ] **Step 10: Commit**

```bash
git add ui/config.go ui/detail.go ui/detail.templ ui/detail_templ.go ui/actions.go ui/actions.templ ui/actions_templ.go ui/handler.go
git commit -m "refactor: remove legacy rendering paths, inbox/ui is now provider-only"
```

---

### Task 6: Update schema provider with respond/complete actions

Now that the shell no longer renders respond/complete buttons, the schema provider needs to include them in its rendered output.

**Files:**
- Modify: `schema/render.templ` (add respond/complete buttons to schema payload)
- Modify: `schema/provider.go` (pass action URLs through render context)

- [ ] **Step 1: Add respond and complete actions to schema render.templ**

The schema provider's `Payload` templ function already renders `schemaActionButton` for schema-defined actions. These buttons POST to `/items/{id}/respond`. Ensure these still work with the inbox endpoint.

Additionally, add a "Complete" button after the action buttons when the item is claimed:

```
// In the Payload template, after action buttons:
if item.Status() == "claimed" {
    @button.Button(button.Props{
        Variant: button.VariantSuccess,
        Size:    button.SizeSm,
        OnClick: ds.Post(basePath + "/items/" + itemID + "/complete"),
    }) {
        Complete
    }
}
```

The `Payload` function signature needs the full `RenderContext` instead of just `itemID` and `basePath`, so update it accordingly.

- [ ] **Step 2: Build and test**

Run: `templ generate && go build ./... && go test -v -count=1 -timeout 120s ./...`
Expected: All tests pass.

- [ ] **Step 3: Commit**

```bash
git add schema/
git commit -m "feat(schema): add respond/complete actions to schema provider"
```

---

### Task 7: Integration smoke test

Verify the full flow works end-to-end with the demo app.

**Files:**
- Modify: `cmd/inboxui/integration_test.go` (update for new architecture)

- [ ] **Step 1: Update integration tests**

Update `TestSchemaRendererIntegration` to verify:
1. Schema items render via the schema ContentProvider
2. Action buttons appear in the provider's rendered content
3. Claim/Release/Cancel buttons appear in the inbox shell
4. The complete flow works (claim → respond → complete)

- [ ] **Step 2: Run integration tests**

Run: `go test -v -count=1 -timeout 120s ./cmd/inboxui/...`
Expected: All integration tests pass.

- [ ] **Step 3: Run full test suite one final time**

Run: `go test -v -count=1 -timeout 120s ./... && go vet ./...`
Expected: All tests pass, no vet warnings.

- [ ] **Step 4: Commit**

```bash
git add cmd/inboxui/integration_test.go
git commit -m "test: update integration tests for content provider architecture"
```
