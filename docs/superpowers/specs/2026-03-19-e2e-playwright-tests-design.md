# E2E Playwright Tests for Inbox UI

## Overview

Full browser E2E tests for the inbox UI using `playwright-go`, `httptest.NewServer`, and testcontainers. All tests written in Go, no Node toolchain required. Playwright drives a real Chromium browser against the running inbox UI.

## File

`cmd/inboxui/e2e_test.go` — single file with `TestE2E` entry point and sequential subtests.

## Test Infrastructure

### Setup (one-time per test run)

1. `TestE2E(t *testing.T)` is the single entry point
2. Postgres via testcontainers (reusing existing `testInbox(t)` pattern from `integration_test.go`)
3. Seed 5 test items with varied statuses, payloads, and tags
4. Build a chi router that replicates the production wiring from `main.go`:
   - `dsx.Middleware(dsx.MiddlewareConfig{Secret: secret})` for CSRF tokens
   - `dsx.Static` served at `/assets/*` for Datastar JS, CSS, and other static assets
   - `inboxui.Handler(ib, ...)` mounted at `/inbox` with options (see Handler Options below)
5. Wrap the router in `httptest.NewServer`
6. Initialize `playwright-go` — launch single Chromium browser
7. Each subtest gets a fresh `BrowserContext` (isolated cookies/storage) and `Page`

### Handler Options

```go
inboxui.Handler(ib,
    inboxui.WithBasePath("/inbox"),
    inboxui.WithPayloadRenderer("google.protobuf.Struct", structRenderer),
    inboxui.WithActor(func(r *http.Request) string {
        return "user:e2e-tester"
    }),
    inboxui.WithFilter(inboxui.FilterConfig{
        Label: "Priority", TagPrefix: "priority:",
        Options: []string{"urgent", "high", "normal", "low"},
    }),
    inboxui.WithFilter(inboxui.FilterConfig{
        Label: "Team", TagPrefix: "team:",
        Options: []string{"compliance", "ops", "finance"},
    }),
    inboxui.WithFilter(inboxui.FilterConfig{
        Label: "Type", TagPrefix: "type:",
        Options: []string{"approval", "review", "input_required"},
    }),
)
```

The `structRenderer` is reused from the existing `cmd/inboxui/payloads.go`.

### Interaction Model: Datastar SSE

The inbox UI uses Datastar for all interactions. This is critical for how tests work:

- **Page loads** (initial navigation) return full HTML pages via `renderPage()`.
- **All subsequent interactions** (clicking items, claiming, filtering, etc.) go through Datastar's SSE mechanism: the browser JS sends an SSE request, and the server responds with DOM patches.
- Clicking a table row triggers `ds.Get(...)` which opens an SSE connection; the server responds with `ds.Send.Drawer(sse, ...)` to patch the drawer into the DOM.
- Action buttons (Claim, Release, Complete, Cancel) trigger `ds.Post(...)` SSE requests; the server responds with SSE DOM patches and toast notifications.
- Filter dropdowns send `ds.Get(...)` SSE requests; the server responds with `sse.PatchElementTempl(queueTable(...))`.

Playwright's built-in auto-waiting (`page.Locator(...).Click()`, `page.Locator(...).WaitFor()`) handles SSE-driven DOM updates naturally — it waits for elements to appear/disappear without explicit sleeps.

### Teardown

Deferred closes for page, browser context, browser, playwright, httptest server. Testcontainers handles Postgres cleanup.

### Actor

Hardcoded to `user:e2e-tester` via `WithActor`. Comment and event assertions check for the display name `e2e-tester` (derived from `actorDisplayName("user:e2e-tester")`).

## Dependencies

- `github.com/playwright-community/playwright-go` added to `go.mod`
- Playwright browsers: `mise.toml` already includes `npm:playwright`. The test calls `playwright.Install()` which downloads Chromium if not already present. If install fails (CI without network), the test calls `t.Skip`.

## Seed Data

| # | Title                     | Tags                                              | Purpose                    |
|---|---------------------------|----------------------------------------------------|----------------------------|
| 1 | Review Q1 Report          | `priority:high`, `team:finance`, `type:review`     | Queue, filters, claim flow |
| 2 | Approve Vendor Contract   | `priority:urgent`, `team:compliance`, `type:approval` | Cancel flow              |
| 3 | Verify Customer Address   | `priority:normal`, `team:ops`, `type:input_required` | Respond + complete flow  |
| 4 | Check Consent Records     | `priority:low`, `team:compliance`, `type:review`   | Comment flow, search       |
| 5 | Process Refund Request    | `priority:high`, `team:finance`, `type:approval`   | Detail drawer              |

All items created as `open` with `system:seed` actor and structpb payloads (so `WithPayloadRenderer("google.protobuf.Struct", ...)` can render them).

## Subtests

Subtests run sequentially — some mutate shared state that later tests depend on. All interactions go through Datastar SSE; assertions wait for DOM patches via Playwright's auto-waiting.

### 1. Queue

- Navigate to `{baseURL}/inbox` (full page load)
- Assert table visible with all 5 items (all are `status:open` at this point)
- Assert column headers present
- Select "high" in the priority filter dropdown — this triggers a Datastar SSE `GET` that patches the table
- Wait for table update, assert only `priority:high` items show (items 1 and 5)
- Clear filter (select empty option), wait for table update, verify all 5 items return

### 2. Detail

- Navigate to `{baseURL}/inbox` (full page load)
- Click the "Process Refund Request" table row — this triggers a Datastar SSE `GET /inbox/items/{id}` that patches the drawer into the DOM
- Wait for the drawer element to become visible (dsx drawer component selector)
- Assert drawer contains correct title, description
- Assert rendered payload is present (structpb payload rendered by `structRenderer`)
- Assert "Created" event visible in the event timeline
- Assert action buttons visible: "Claim" and "Cancel"

### 3. ClaimRelease

- Navigate to `{baseURL}/inbox`, click "Review Q1 Report" row, wait for drawer
- Click "Claim" button — triggers `ds.Post(.../claim)`, server responds with SSE patches updating the drawer
- Wait for drawer to update: assert status badge shows "claimed"
- Assert "Release" and "Complete" buttons appear, "Claim" button gone
- Click "Release" — triggers `ds.Post(.../release)`, server responds with SSE patches
- Wait for drawer to update: assert status badge shows "open", "Claim" button reappears

### 4. MyWork

- Navigate to `{baseURL}/inbox`, click "Review Q1 Report" row, wait for drawer
- Claim it (click Claim, wait for status → "claimed")
- Click "My Work" tab link — full page navigation to `{baseURL}/inbox/mywork`
- Assert "Review Q1 Report" appears in the my-work table
- Navigate to `{baseURL}/inbox` (Queue) — assert "Review Q1 Report" is NOT in the queue (queue filters `status:open`, claimed items are excluded)

### 5. RespondComplete

- Navigate to `{baseURL}/inbox`, click "Verify Customer Address" row, wait for drawer
- Click "Claim", wait for status → "claimed"
- Click "Respond" button — toggles open the respond form (Datastar signal toggle, no SSE)
- Fill "action" input with "approve", fill "comment" textarea with "Looks good"
- Click "Submit" — triggers `ds.Post(.../respond)` with filtered signals, server responds with SSE patches
- Wait for drawer update: assert "responded" event appears in timeline
- Click "Complete" — triggers `ds.Post(.../complete)`, server responds with SSE patches
- Assert status → "completed", no action buttons visible (terminal state)

### 6. Cancel

- Navigate to `{baseURL}/inbox`, click "Approve Vendor Contract" row, wait for drawer
- Click "Cancel" button — toggles open the cancel form (Datastar signal toggle, no SSE)
- Fill reason textarea with "No longer needed"
- Click "Confirm Cancel" — triggers `ds.Post(.../cancel)` with filtered signals
- Wait for drawer update: assert status → "cancelled", no action buttons visible (terminal state)

### 7. Comment

- Navigate to `{baseURL}/inbox`, click "Check Consent Records" row, wait for drawer
- Fill the comment textarea (bound to `comment-form.body`) with "Test comment from E2E"
- Click "Comment" button — triggers `ds.Post(.../comment)` with filtered signals
- Wait for drawer update: assert comment appears in event timeline
- Assert comment body matches "Test comment from E2E"
- Assert actor displays as "e2e-tester"

### 8. Search

- Navigate to `{baseURL}/inbox/search` (full page load)
- Fill the search input (bound to `search.q`) with "Refund"
- Click "Search" button (form submit triggers `ds.Get(.../search)` with filtered signals — but since `handleSearch` reads `r.URL.Query().Get("q")`, the test navigates directly to `{baseURL}/inbox/search?q=Refund` as a full page load instead)
- Assert "Process Refund Request" appears in results
- Assert other items (e.g. "Review Q1 Report") do not appear

Note: The search handler reads `q` from URL query params directly. Datastar's filtered signals for GET requests may encode the query differently. To keep the test reliable, use direct navigation with `?q=Refund` rather than relying on the Datastar form submit path.

## Helpers

Defined within `e2e_test.go`:

- `seedItems(t, ib)` — creates the 5 seed items with structpb payloads, returns IDs
- `newPage(t, bCtx, baseURL)` — creates page, navigates, returns with `t.Cleanup`
- `clickItem(page, title)` — clicks table row by matching title text content, waits for drawer
- `assertStatus(t, page, expected)` — checks status badge text in the detail drawer
- `waitForDrawer(page)` — waits for the dsx drawer DOM element to be visible
- `submitComment(page, text)` — fills comment textarea via Datastar signal binding, clicks Comment button, waits for timeline update

## No Build Tag

Tests run with `go test ./cmd/inboxui/...`. Self-skip via `t.Skip` if Docker or browsers unavailable.
