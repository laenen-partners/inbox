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
4. Create chi handler via `inboxui.Handler(ib, ...)` with filters, renderers, actor config
5. Wrap in `httptest.NewServer`
6. Initialize `playwright-go` — launch single Chromium browser
7. Each subtest gets a fresh `BrowserContext` (isolated cookies/storage) and `Page`

### Teardown

Deferred closes for page, browser context, browser, playwright, httptest server. Testcontainers handles Postgres cleanup.

### Actor

Hardcoded to `user:e2e-tester` via `WithActor`.

## Dependencies

- `github.com/playwright-community/playwright-go` added to `go.mod`
- Playwright browsers installed locally via `playwright.Install()` (auto-downloads Chromium on first run, skips if unavailable)

## Seed Data

| # | Title                     | Tags                                              | Purpose                    |
|---|---------------------------|----------------------------------------------------|----------------------------|
| 1 | Review Q1 Report          | `priority:high`, `team:finance`, `type:review`     | Queue, filters, claim flow |
| 2 | Approve Vendor Contract   | `priority:urgent`, `team:compliance`, `type:approval` | Cancel flow              |
| 3 | Verify Customer Address   | `priority:normal`, `team:ops`, `type:input_required` | Respond + complete flow  |
| 4 | Check Consent Records     | `priority:low`, `team:compliance`, `type:review`   | Comment flow, search       |
| 5 | Process Refund Request    | `priority:high`, `team:finance`, `type:approval`   | Detail drawer, SSE test    |

All items created as `open` with `system:seed` actor and structpb payloads.

## Subtests

Subtests run sequentially — some mutate shared state that later tests depend on.

### 1. Queue

- Navigate to `/inbox`
- Assert table visible with all 5 items
- Assert column headers present
- Select "high" priority filter, assert only `priority:high` items show
- Clear filter, verify all items return

### 2. Detail

- Navigate to `/inbox`, click "Process Refund Request"
- Assert drawer opens with correct title, description, rendered payload
- Assert "Created" event in timeline
- Assert action buttons visible (Claim, Cancel)

### 3. ClaimRelease

- Navigate to `/inbox`, click "Review Q1 Report"
- Click Claim, assert status → "claimed", Release button appears
- Click Release, assert status → "open"

### 4. MyWork

- Claim "Review Q1 Report" again
- Click "My Work" tab, assert item appears
- Navigate back to Queue, assert item shows as claimed

### 5. RespondComplete

- Navigate to `/inbox`, click "Verify Customer Address"
- Claim it
- Fill and submit response form
- Assert "responded" event in timeline
- Click Complete, assert status → "completed", no action buttons

### 6. Cancel

- Navigate to `/inbox`, click "Approve Vendor Contract"
- Click Cancel (with reason if prompted)
- Assert status → "cancelled", no action buttons

### 7. Comment

- Navigate to `/inbox`, click "Check Consent Records"
- Type and submit a comment
- Assert comment appears in event timeline with correct actor and body

### 8. Search

- Click "Search" tab
- Type "Refund", assert "Process Refund Request" appears
- Assert unrelated items absent

### 9. SSE

- Navigate to `/inbox`, open "Process Refund Request" detail
- In a goroutine, call `ib.Comment(...)` via Go API
- Assert new comment appears in drawer without page reload (Playwright auto-waiting)

## Helpers

Defined within `e2e_test.go`:

- `seedItems(t, ib)` — creates the 5 seed items, returns IDs
- `newPage(t, bCtx, baseURL)` — creates page, navigates, returns with `t.Cleanup`
- `clickItem(page, title)` — clicks table row by title text
- `assertStatus(t, page, expected)` — checks status badge in detail drawer
- `waitForDrawer(page)` — waits for drawer element visibility
- `submitComment(page, text)` — fills and submits comment form

## No Build Tag

Tests run with `go test ./cmd/inboxui/...`. Self-skip via `t.Skip` if Docker or browsers unavailable.
