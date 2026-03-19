# Inbox UI Design

## Overview

A dsx-based UI for the inbox system, targeting operations staff who process work items (claim, respond, complete) as their daily workflow. Ships as a Go package (`inbox/ui/`) exporting a mountable `chi.Router`, plus a standalone binary (`cmd/inboxui/`) for independent use.

## Architecture

### Embedding API

The UI exports a single entry point:

```go
package ui

func Handler(ib *inbox.Inbox, opts ...Option) chi.Router

type Option func(*config)

func WithActor(fn func(r *http.Request) string)
func WithFilter(f FilterConfig)
func WithPayloadRenderer(payloadType string, fn PayloadRendererFunc)
func WithBasePath(path string)
```

**Embedding in an app:**

```go
r := chi.NewRouter()
r.Mount("/inbox", ui.Handler(ib,
    ui.WithActor(func(r *http.Request) string {
        return "user:" + auth.UserFrom(r.Context())
    }),
    ui.WithFilter(ui.FilterConfig{
        Label:     "Team",
        TagPrefix: "team:",
        Options:   []string{"compliance", "ops", "finance"},
    }),
    ui.WithFilter(ui.FilterConfig{
        Label:     "Priority",
        TagPrefix: "priority:",
        Options:   []string{"urgent", "high", "normal", "low"},
    }),
))
```

The standalone binary (`cmd/inboxui/main.go`) wraps this with env-based config for DB connection, default filters, and static asset serving.

### Routes

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Queue view (HTML page) |
| GET | `/mywork` | My claimed items |
| GET | `/search` | Search view |
| GET | `/items/{id}` | Item detail (drawer fragment via Datastar) |
| POST | `/items/{id}/claim` | Claim action |
| POST | `/items/{id}/release` | Release action |
| POST | `/items/{id}/respond` | Respond action |
| POST | `/items/{id}/complete` | Complete action |
| POST | `/items/{id}/cancel` | Cancel action |
| POST | `/items/{id}/comment` | Add comment |

All mutation endpoints return SSE fragments (Datastar pattern) to update the UI in-place. The actor for each action comes from `WithActor`.

### Data flow

All interactivity uses Datastar — no full page reloads after initial load.

1. Initial page load: server renders full HTML with first page of results.
2. Filter change: Datastar `@get` to `/` with filter query params — server returns SSE fragment replacing the table body.
3. Pagination: same pattern, cursor passed as query param.
4. Drawer open: Datastar `@get` to `/items/{id}` — returns SSE fragment populating the drawer.
5. Actions: Datastar `@post` — returns SSE fragments updating drawer content, queue row, and showing toast.

## Screens

### Queue View (GET `/`)

The main working screen. Filterable, paginated table of inbox items.

**Layout:**
- Top bar: preset filter dropdowns + active filter badges
- Table columns: Title, Status, Priority, Team, Assignee, Age, Deadline
- Column values extracted from tags (e.g. `priority:` tag -> Priority column)
- Rows are clickable — opens the detail drawer
- Pagination at bottom (cursor-based, matching `ListByTags`)

**Behavior:**
- Default view: all `status:open` items matching configured filters.
- Selecting a filter updates the list via Datastar `@get` — no full page reload.
- Active filters shown as dismissible badges below the filter bar.
- Rows with `priority:urgent` get a visual accent (badge or row highlight).
- Relative timestamps for age ("2h ago", "3d ago").
- Stale items (no activity for configurable threshold) get a subtle visual indicator.

**Filter bar:**

Each dropdown is configured via `WithFilter`. Selecting a value adds the corresponding tag to the `ListByTags` query. Multiple filters combine with AND (matching the backend behavior).

### Item Detail Drawer (GET `/items/{id}`)

Opens from the right when an operator clicks a queue row. The queue stays visible (dimmed) behind it. Uses the dsx `drawer` component (end/right position).

**Layout (top to bottom):**
1. **Header:** Title, status badge, close button.
2. **Meta bar:** Assignee, team, priority, deadline (all from tags), age.
3. **Payload section:** Rendered by the registered payload renderer for this `PayloadType`, or JSON fallback via dsx `jsonview`.
4. **Actions bar:** Context-sensitive buttons based on current status.
5. **Activity feed:** Chronological event log using dsx `feed`/`feeditem` components.

**Activity feed entries show:**
- Event type as human-readable label ("Claimed by", "Comment", "Escalated to").
- Actor name.
- Detail/comment text.
- Relative timestamp.
- Type-specific details: comment body, respond action + comment, escalation from/to teams, added/removed tag badges.

**Behavior:**
- After any action, the drawer content refreshes via SSE to reflect updated state.
- Closing the drawer also refreshes the queue table row (status may have changed).

**Payload renderer registry:**

Apps register custom renderers per `PayloadType`:
```go
ui.WithPayloadRenderer("myapp.v1.InvoicePayload", invoiceRenderer)
```

The renderer receives unpacked proto bytes and returns a `templ.Component`. Unregistered types fall back to dsx `jsonview`.

### My Work (GET `/mywork`)

- Same table layout as the queue view.
- Pre-filtered to items where `assignee:` tag matches the current actor AND `status:claimed`.
- Same filter bar available for further narrowing.
- Clicking a row opens the detail drawer.
- Empty state when no claimed items, nudging the operator to the queue.

### Search (GET `/search`)

- Text input at top for fuzzy search.
- Results in the same table format as the queue.
- Uses `inbox.Search()` (token-based fuzzy match on title + description).
- Search fires on submit via Datastar `@get` with query param.
- Paginated same as queue view.
- Clicking a result opens the detail drawer.

Semantic search is not exposed in the UI — it's an API-level feature for programmatic use.

## Actions & Feedback

Actions are context-sensitive buttons in the detail drawer.

### Action visibility

| Status | Current user is claimant? | Available actions |
|--------|--------------------------|-------------------|
| `open` | — | Claim, Cancel |
| `claimed` | yes | Respond, Complete, Release, Cancel, Comment |
| `claimed` | no | Comment only |
| `completed` | — | Comment only |
| `cancelled` | — | Comment only |
| `expired` | — | Comment only |

### Respond action

Opens an inline form within the drawer:
- Action field (text input — e.g. "approve", "reject", "request-info").
- Comment field (textarea).
- Submit button.

### Comment action

Always visible as a textarea + button at the bottom of the activity feed.

### Feedback

All actions use Datastar `@post` and return SSE fragments that:
1. Update the drawer content (new status, new event in feed).
2. Show a dsx `alert` toast (auto-dismiss) confirming the action.
3. Update the corresponding queue row (status badge change).

No confirmation modals — everything is inline and immediate.

## File Layout

```
inbox/
  ui/
    config.go          — Option, config, FilterConfig, PayloadRenderer types
    handler.go         — Handler() constructor, chi.Router wiring, middleware
    queue.go           — queue list handler (GET /)
    queue.templ        — queue table, filter bar, pagination
    mywork.go          — my work handler (GET /mywork)
    mywork.templ       — my work view (reuses queue table component)
    search.go          — search handler (GET /search)
    search.templ       — search input + results
    detail.go          — item detail handler (GET /items/{id})
    detail.templ       — drawer content: header, meta, payload, actions, feed
    actions.go         — action handlers (POST /items/{id}/{action})
    actions.templ      — action buttons, respond form, comment form
    toast.templ        — toast/alert feedback component
    helpers.go         — shared helpers (tag extraction, time formatting, actor display)
  cmd/
    inboxui/
      main.go          — standalone binary, env-based config
      testdata/        — test fixtures (seed data, proto payloads)
      e2e/
        playwright.config.ts
        tests/
          queue.spec.ts      — queue filtering, pagination, row click
          detail.spec.ts     — drawer open/close, payload rendering, actions
          actions.spec.ts    — claim, respond, complete, cancel, comment flows
          mywork.spec.ts     — my work filtering, empty state
          search.spec.ts     — search input, results, navigation
```

Each `.go` handler file pairs with a `.templ` file. Handlers parse requests, call the inbox API, and return Datastar SSE fragments. Templ files define the components.

## Technology

- **Go + Chi** for routing and HTTP handling.
- **Templ** for server-side HTML templating.
- **Datastar** for client-side interactivity (SSE fragments, signals).
- **dsx** component library (DaisyUI-based) for all UI elements.
- **Playwright** for e2e testing of the standalone binary.
- **Testcontainers** for test database provisioning.

## Key dsx components used

- `drawer` — item detail panel
- `table` — queue/search results (if available, otherwise raw HTML table with DaisyUI classes)
- `button` — all action buttons
- `badge` — status, priority, tag display
- `alert` — toast feedback
- `dropdown` — filter selects
- `feed`/`feeditem` — activity event log
- `jsonview` — fallback payload renderer
- `form` — respond and comment forms
- `navbar` — top navigation between queue/mywork/search

## Testing strategy

### Playwright e2e tests

Run against the standalone binary with a testcontainers Postgres database, seeded with sample inbox items.

**Test coverage:**
- Queue: filtering, pagination, row click opens drawer.
- Detail: drawer open/close, payload rendering, action button visibility by status.
- Actions: full claim -> respond -> complete flow, cancel flow, comment flow.
- My Work: shows only claimed items for current user, empty state.
- Search: text search returns matching items, result click opens drawer.

The standalone binary accepts a flag or env var to seed test data on startup.
