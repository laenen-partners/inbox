# E2E Playwright Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Full browser E2E tests for the inbox UI using `playwright-go`, `httptest.NewServer`, and testcontainers.

**Architecture:** Single `TestE2E` function in `cmd/inboxui/e2e_test.go` spins up Postgres via testcontainers, builds a chi router matching production wiring (CSRF, static assets, inbox handler), wraps it in `httptest.NewServer`, launches Chromium via `playwright-go`, and runs sequential subtests. Each subtest gets a fresh `BrowserContext` for cookie isolation. The UI uses Datastar SSE for all interactions — Playwright's auto-waiting handles DOM patches naturally.

**Tech Stack:** Go, playwright-go, testcontainers-go (PostgreSQL), httptest, chi, dsx, Datastar

**Spec:** `docs/superpowers/specs/2026-03-19-e2e-playwright-tests-design.md`

---

## File Structure

- **Create:** `cmd/inboxui/e2e_test.go` — all E2E tests, helpers, and seed data
- **Modify:** `go.mod` / `go.sum` — add `playwright-go` dependency

## Key DOM Selectors Reference

These selectors are derived from the templ source and dsx library:

| Element | Selector |
|---------|----------|
| Queue table | `#queue-table` |
| Table rows | `#queue-table tbody tr[id^='row-']` |
| Row by title | `#queue-table tr:has-text("Title")` |
| Filter selects | `#queue-filters select` |
| Drawer container | `#drawer-panel` |
| Drawer title (h2) | `#drawer-panel h2` |
| Status badge in drawer | `#drawer-panel .badge` |
| Event in timeline | `#drawer-panel .flex.gap-3.py-2` |
| Event label | `.font-medium.text-sm` (inside event) |
| Event actor | `.text-xs.text-base-content\\/60` (inside event) |
| Claim button | `#drawer-panel button:has-text("Claim")` |
| Release button | `#drawer-panel button:has-text("Release")` |
| Complete button | `#drawer-panel button:has-text("Complete")` |
| Cancel button (toggle) | `#drawer-panel button:has-text("Cancel")` |
| Confirm Cancel button | `#drawer-panel button:has-text("Confirm Cancel")` |
| Respond button (toggle) | `#drawer-panel button:has-text("Respond")` |
| Submit button (respond form) | `#drawer-panel button:has-text("Submit")` |
| Comment button | `#drawer-panel button:has-text("Comment")` |
| Cancel reason textarea | `textarea[placeholder="Reason for cancellation"]` |
| Respond action input | `input[placeholder="Action (e.g. approve, reject)"]` |
| Respond comment textarea | `#drawer-panel div[data-signals] textarea[placeholder="Comment"]` |
| Comment body textarea | `textarea[placeholder="Write a comment..."]` |
| Tab links | `a[role="tab"]` |
| Item count text | `#queue-filters .text-sm` |

---

### Task 1: Add playwright-go dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add playwright-go to go.mod**

```bash
cd /Users/peteresztari/projects/laenen-partners/inbox
go get github.com/playwright-community/playwright-go
```

- [ ] **Step 2: Install Playwright browsers**

```bash
go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --with-deps chromium
```

- [ ] **Step 3: Verify go.mod contains the dependency**

```bash
grep playwright go.mod
```

Expected: line containing `github.com/playwright-community/playwright-go`

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add playwright-go for E2E browser tests"
```

---

### Task 2: Create e2e_test.go with TestE2E skeleton

**Files:**
- Create: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Write the test skeleton with infrastructure setup**

Create `cmd/inboxui/e2e_test.go`:

```go
package main

import (
	"context"
	"crypto/rand"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/playwright-community/playwright-go"

	"github.com/laenen-partners/dsx"
	"github.com/laenen-partners/inbox"
	inboxui "github.com/laenen-partners/inbox/ui"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestE2E(t *testing.T) {
	if err := playwright.Install(&playwright.RunOptions{
		Browsers: []string{"chromium"},
	}); err != nil {
		t.Skipf("playwright browsers not available: %v", err)
	}

	ib := testInbox(t)
	seedE2EItems(t, ib)

	// Build router matching production wiring (main.go)
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("generate secret: %v", err)
	}

	r := chi.NewRouter()
	r.Use(dsx.Middleware(dsx.MiddlewareConfig{Secret: secret}))

	staticFS, _ := fs.Sub(dsx.Static, "static")
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServerFS(staticFS)))

	r.Mount("/inbox", inboxui.Handler(ib,
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
	))

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("start playwright: %v", err)
	}
	t.Cleanup(func() { pw.Stop() })

	browser, err := pw.Chromium.Launch()
	if err != nil {
		t.Fatalf("launch browser: %v", err)
	}
	t.Cleanup(func() { browser.Close() })

	baseURL := srv.URL

	t.Run("Queue", func(t *testing.T) {
		t.Log("placeholder")
	})
	t.Run("Detail", func(t *testing.T) {
		t.Log("placeholder")
	})
	t.Run("ClaimRelease", func(t *testing.T) {
		t.Log("placeholder")
	})
	t.Run("MyWork", func(t *testing.T) {
		t.Log("placeholder")
	})
	t.Run("RespondComplete", func(t *testing.T) {
		t.Log("placeholder")
	})
	t.Run("Cancel", func(t *testing.T) {
		t.Log("placeholder")
	})
	t.Run("Comment", func(t *testing.T) {
		t.Log("placeholder")
	})
	t.Run("Search", func(t *testing.T) {
		t.Log("placeholder")
	})

	// Suppress unused variable warnings until subtests use these
	_ = baseURL
	_ = browser
	_ = ib
}
```

Note: `testInbox(t)` is defined in `integration_test.go` (same package). `structRenderer` is defined in `payloads.go` (same package).

- [ ] **Step 2: Add stub for seedE2EItems and helpers**

Append to the same file:

```go
func seedE2EItems(t *testing.T, ib *inbox.Inbox) {
	t.Helper()
	// Will be filled in Task 3
}

func e2eNewPage(t *testing.T, browser playwright.Browser, url string) playwright.Page {
	t.Helper()
	bCtx, err := browser.NewContext()
	if err != nil {
		t.Fatalf("create browser context: %v", err)
	}
	t.Cleanup(func() { bCtx.Close() })

	page, err := bCtx.NewPage()
	if err != nil {
		t.Fatalf("create page: %v", err)
	}

	if _, err := page.Goto(url); err != nil {
		t.Fatalf("navigate to %s: %v", url, err)
	}
	return page
}
```

- [ ] **Step 3: Verify compilation**

```bash
go vet ./cmd/inboxui/
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E test skeleton with playwright + httptest setup"
```

---

### Task 3: Add seed data and helper functions

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Implement seedE2EItems**

Replace the stub `seedE2EItems` with:

```go
// e2eItemTitles are the titles of the 5 seed items, in creation order.
// Subtests reference items by these titles.
var e2eItemTitles = [5]string{
	"Review Q1 Report",
	"Approve Vendor Contract",
	"Verify Customer Address",
	"Check Consent Records",
	"Process Refund Request",
}

func seedE2EItems(t *testing.T, ib *inbox.Inbox) {
	t.Helper()
	ctx := context.Background()

	items := []inbox.Meta{
		{
			Title:       e2eItemTitles[0],
			Description: "Please review the Q1 financial report for accuracy.",
			Actor:       "system:seed",
			Tags:        []string{"priority:high", "team:finance", "type:review"},
		},
		{
			Title:       e2eItemTitles[1],
			Description: "Vendor contract requires compliance approval.",
			Actor:       "system:seed",
			Tags:        []string{"priority:urgent", "team:compliance", "type:approval"},
		},
		{
			Title:       e2eItemTitles[2],
			Description: "Customer address needs verification for KYC.",
			Actor:       "system:seed",
			Tags:        []string{"priority:normal", "team:ops", "type:input_required"},
		},
		{
			Title:       e2eItemTitles[3],
			Description: "Consent records need to be reviewed for GDPR compliance.",
			Actor:       "system:seed",
			Tags:        []string{"priority:low", "team:compliance", "type:review"},
		},
		{
			Title:       e2eItemTitles[4],
			Description: "Customer refund request needs approval.",
			Actor:       "system:seed",
			Tags:        []string{"priority:high", "team:finance", "type:approval"},
		},
	}

	for i := range items {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"_type":   "address_request",
			"message": items[i].Description,
			"street":  "",
			"city":    "",
			"zip":     "",
			"country": "",
		})
		items[i].Payload = payload

		if _, err := ib.Create(ctx, items[i]); err != nil {
			t.Fatalf("seed item %d (%s): %v", i, items[i].Title, err)
		}
	}
}
```

- [ ] **Step 2: Implement helper functions**

Replace the `e2eNewPage` stub with the full set of helpers:

```go
func e2eNewPage(t *testing.T, browser playwright.Browser, url string) playwright.Page {
	t.Helper()
	bCtx, err := browser.NewContext()
	if err != nil {
		t.Fatalf("create browser context: %v", err)
	}
	t.Cleanup(func() { bCtx.Close() })

	page, err := bCtx.NewPage()
	if err != nil {
		t.Fatalf("create page: %v", err)
	}

	if _, err := page.Goto(url); err != nil {
		t.Fatalf("navigate to %s: %v", url, err)
	}
	return page
}

// e2eClickItem clicks a row in the queue table by matching the item title,
// then waits for the drawer to open.
func e2eClickItem(t *testing.T, page playwright.Page, title string) {
	t.Helper()
	row := page.Locator("#queue-table tr:has-text('" + title + "')")
	if err := row.Click(); err != nil {
		t.Fatalf("click item %q: %v", title, err)
	}
	e2eWaitForDrawer(t, page)
}

// e2eWaitForDrawer waits for the drawer panel to contain content (the h2 title).
func e2eWaitForDrawer(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := page.Locator("#drawer-panel h2").WaitFor(); err != nil {
		t.Fatalf("wait for drawer: %v", err)
	}
}

// e2eAssertStatus checks that the status badge in the open drawer matches the expected value.
func e2eAssertStatus(t *testing.T, page playwright.Page, want string) {
	t.Helper()
	badge := page.Locator("#drawer-panel .badge")
	got, err := badge.TextContent()
	if err != nil {
		t.Fatalf("read status badge: %v", err)
	}
	got = trimBadgeText(got)
	if got != want {
		t.Errorf("status: got %q, want %q", got, want)
	}
}

// e2eWaitForStatus waits for the status badge to show the expected value.
// Used after SSE-driven actions that re-render the drawer.
func e2eWaitForStatus(t *testing.T, page playwright.Page, status string) {
	t.Helper()
	loc := page.Locator("#drawer-panel .badge:has-text('" + status + "')")
	if err := loc.WaitFor(); err != nil {
		t.Fatalf("wait for status %q: %v", status, err)
	}
}

// e2eClickButton clicks a button inside the drawer by its text content.
func e2eClickButton(t *testing.T, page playwright.Page, label string) {
	t.Helper()
	btn := page.Locator("#drawer-panel button:has-text('" + label + "')")
	if err := btn.Click(); err != nil {
		t.Fatalf("click button %q: %v", label, err)
	}
}

// trimBadgeText trims whitespace from single-word badge text content.
func trimBadgeText(s string) string {
	return strings.TrimSpace(s)
}
```

- [ ] **Step 3: Verify compilation**

```bash
go vet ./cmd/inboxui/
```

Expected: no errors. The `_ = baseURL`, `_ = browser`, `_ = ib` lines suppress unused-variable errors. They will be removed in Task 4 when the first real subtest uses these variables.

- [ ] **Step 4: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E seed data and helper functions"
```

---

### Task 4: Queue subtest

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Remove `_ = ...` suppression lines from TestE2E**

Remove these lines from the end of `TestE2E` (no longer needed — the subtests now use these variables):
```go
	_ = baseURL
	_ = browser
	_ = ib
```

- [ ] **Step 2: Replace the Queue placeholder subtest**

```go
	t.Run("Queue", func(t *testing.T) {
		page := e2eNewPage(t, browser, baseURL+"/inbox")

		// Table should be visible with all 5 items
		table := page.Locator("#queue-table")
		if err := table.WaitFor(); err != nil {
			t.Fatalf("queue table not visible: %v", err)
		}

		rows := page.Locator("#queue-table tbody tr[id^='row-']")
		count, err := rows.Count()
		if err != nil {
			t.Fatalf("count rows: %v", err)
		}
		if count != 5 {
			t.Errorf("row count: got %d, want 5", count)
		}

		// Column headers
		for _, col := range []string{"Title", "Status", "Priority", "Team", "Assignee", "Age", "Deadline"} {
			th := page.Locator("#queue-table th:has-text('" + col + "')")
			visible, err := th.IsVisible()
			if err != nil {
				t.Fatalf("check header %q: %v", col, err)
			}
			if !visible {
				t.Errorf("header %q not visible", col)
			}
		}

		// Filter by priority:high
		prioritySelect := page.Locator("#queue-filters select").First()
		if _, err := prioritySelect.SelectOption(playwright.SelectOptionValues{
			Values: playwright.StringSlice("high"),
		}); err != nil {
			t.Fatalf("select priority filter: %v", err)
		}

		// Wait for filtered table — items without priority:high should disappear
		hidden := page.Locator("#queue-table tr:has-text('" + e2eItemTitles[1] + "')")
		if err := hidden.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateHidden,
		}); err != nil {
			t.Fatalf("wait for filter: %v", err)
		}

		// Should have exactly 2 rows (items 0 and 4 have priority:high)
		filteredCount, err := page.Locator("#queue-table tbody tr[id^='row-']").Count()
		if err != nil {
			t.Fatalf("count filtered rows: %v", err)
		}
		if filteredCount != 2 {
			t.Errorf("filtered row count: got %d, want 2", filteredCount)
		}

		// Clear filter
		if _, err := prioritySelect.SelectOption(playwright.SelectOptionValues{
			Values: playwright.StringSlice(""),
		}); err != nil {
			t.Fatalf("clear priority filter: %v", err)
		}

		// Wait for all items to reappear
		reappeared := page.Locator("#queue-table tr:has-text('" + e2eItemTitles[1] + "')")
		if err := reappeared.WaitFor(); err != nil {
			t.Fatalf("wait for unfilter: %v", err)
		}

		allCount, err := page.Locator("#queue-table tbody tr[id^='row-']").Count()
		if err != nil {
			t.Fatalf("count all rows: %v", err)
		}
		if allCount != 5 {
			t.Errorf("unfiltered row count: got %d, want 5", allCount)
		}
	})
```

- [ ] **Step 3: Run the test**

```bash
go test -v -count=1 -timeout 120s -run TestE2E/Queue ./cmd/inboxui/
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E Queue subtest with filter verification"
```

---

### Task 5: Detail subtest

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Replace the Detail placeholder subtest**

```go
	t.Run("Detail", func(t *testing.T) {
		page := e2eNewPage(t, browser, baseURL+"/inbox")

		// Click "Process Refund Request" to open drawer
		e2eClickItem(t, page, e2eItemTitles[4])

		// Check title
		title, err := page.Locator("#drawer-panel h2").TextContent()
		if err != nil {
			t.Fatalf("read drawer title: %v", err)
		}
		title = strings.TrimSpace(title)
		if title != e2eItemTitles[4] {
			t.Errorf("drawer title: got %q, want %q", title, e2eItemTitles[4])
		}

		// Check status badge shows "open"
		e2eAssertStatus(t, page, "open")

		// Check description is visible
		desc := page.Locator("#drawer-panel:has-text('Customer refund request needs approval.')")
		visible, err := desc.IsVisible()
		if err != nil {
			t.Fatalf("check description: %v", err)
		}
		if !visible {
			t.Error("description not visible in drawer")
		}

		// Check payload section exists
		payloadHeading := page.Locator("#drawer-panel h3:has-text('Payload')")
		visible, err = payloadHeading.IsVisible()
		if err != nil {
			t.Fatalf("check payload heading: %v", err)
		}
		if !visible {
			t.Error("payload heading not visible")
		}

		// Check "Created" event in activity feed
		createdEvent := page.Locator("#drawer-panel .font-medium.text-sm:has-text('Created')")
		visible, err = createdEvent.IsVisible()
		if err != nil {
			t.Fatalf("check created event: %v", err)
		}
		if !visible {
			t.Error("'Created' event not visible in activity feed")
		}

		// Check action buttons: Claim and Cancel should be visible (item is open)
		for _, btn := range []string{"Claim", "Cancel"} {
			loc := page.Locator("#drawer-panel button:has-text('" + btn + "')")
			visible, err = loc.IsVisible()
			if err != nil {
				t.Fatalf("check %s button: %v", btn, err)
			}
			if !visible {
				t.Errorf("%s button not visible", btn)
			}
		}
	})
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 -timeout 120s -run TestE2E/Detail ./cmd/inboxui/
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E Detail subtest — drawer content verification"
```

---

### Task 6: ClaimRelease subtest

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Replace the ClaimRelease placeholder subtest**

```go
	t.Run("ClaimRelease", func(t *testing.T) {
		page := e2eNewPage(t, browser, baseURL+"/inbox")

		e2eClickItem(t, page, e2eItemTitles[0]) // "Review Q1 Report"
		e2eAssertStatus(t, page, "open")

		// Claim
		e2eClickButton(t, page, "Claim")
		e2eWaitForStatus(t, page, "claimed")

		// Release button should appear, Claim button should be gone
		release := page.Locator("#drawer-panel button:has-text('Release')")
		if err := release.WaitFor(); err != nil {
			t.Fatalf("Release button not visible after claim: %v", err)
		}
		claim := page.Locator("#drawer-panel button:has-text('Claim')")
		visible, err := claim.IsVisible()
		if err != nil {
			t.Fatalf("check Claim button visibility: %v", err)
		}
		if visible {
			t.Error("Claim button should not be visible after claiming")
		}

		// Release
		e2eClickButton(t, page, "Release")
		e2eWaitForStatus(t, page, "open")

		// Claim button should reappear
		if err := claim.WaitFor(); err != nil {
			t.Fatalf("Claim button not visible after release: %v", err)
		}
	})
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 -timeout 120s -run TestE2E/ClaimRelease ./cmd/inboxui/
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E ClaimRelease subtest"
```

---

### Task 7: MyWork subtest

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

Note: This test depends on the item being in `open` state from ClaimRelease (which releases it at the end).

- [ ] **Step 1: Replace the MyWork placeholder subtest**

```go
	t.Run("MyWork", func(t *testing.T) {
		page := e2eNewPage(t, browser, baseURL+"/inbox")

		// Claim "Review Q1 Report"
		e2eClickItem(t, page, e2eItemTitles[0])
		e2eClickButton(t, page, "Claim")
		e2eWaitForStatus(t, page, "claimed")

		// Navigate to My Work tab
		myWorkTab := page.Locator("a[role='tab']:has-text('My Work')")
		if err := myWorkTab.Click(); err != nil {
			t.Fatalf("click My Work tab: %v", err)
		}

		// Wait for page to load — My Work reuses queueTable
		if err := page.Locator("#queue-table").WaitFor(); err != nil {
			t.Fatalf("my work table not visible: %v", err)
		}

		// "Review Q1 Report" should appear in My Work
		myItem := page.Locator("#queue-table tr:has-text('" + e2eItemTitles[0] + "')")
		visible, err := myItem.IsVisible()
		if err != nil {
			t.Fatalf("check item in my work: %v", err)
		}
		if !visible {
			t.Error("claimed item not visible in My Work")
		}

		// Navigate back to Queue
		queueTab := page.Locator("a[role='tab']:has-text('Queue')")
		if err := queueTab.Click(); err != nil {
			t.Fatalf("click Queue tab: %v", err)
		}
		if err := page.Locator("#queue-table").WaitFor(); err != nil {
			t.Fatalf("queue table not visible: %v", err)
		}

		// Claimed item should NOT appear in Queue (filters status:open)
		queueItem := page.Locator("#queue-table tr:has-text('" + e2eItemTitles[0] + "')")
		if err := queueItem.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateHidden,
		}); err != nil {
			t.Fatalf("wait for item hidden in queue: %v", err)
		}
	})
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 -timeout 120s -run TestE2E/MyWork ./cmd/inboxui/
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E MyWork subtest — claimed items appear in My Work"
```

---

### Task 8: RespondComplete subtest

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Replace the RespondComplete placeholder subtest**

```go
	t.Run("RespondComplete", func(t *testing.T) {
		page := e2eNewPage(t, browser, baseURL+"/inbox")

		e2eClickItem(t, page, e2eItemTitles[2]) // "Verify Customer Address"
		e2eAssertStatus(t, page, "open")

		// Claim
		e2eClickButton(t, page, "Claim")
		e2eWaitForStatus(t, page, "claimed")

		// Click Respond to toggle open the form
		e2eClickButton(t, page, "Respond")

		// Fill respond form fields
		actionInput := page.Locator("input[placeholder='Action (e.g. approve, reject)']")
		if err := actionInput.WaitFor(); err != nil {
			t.Fatalf("respond form not visible: %v", err)
		}
		if err := actionInput.Fill("approve"); err != nil {
			t.Fatalf("fill action: %v", err)
		}

		commentInput := page.Locator("#drawer-panel div[data-signals] textarea[placeholder='Comment']")
		if err := commentInput.Fill("Looks good"); err != nil {
			t.Fatalf("fill respond comment: %v", err)
		}

		// Submit the response
		e2eClickButton(t, page, "Submit")

		// Wait for "Responded" event in the activity feed
		respondedEvent := page.Locator("#drawer-panel .font-medium.text-sm:has-text('Responded')")
		if err := respondedEvent.WaitFor(); err != nil {
			t.Fatalf("Responded event not visible: %v", err)
		}

		// Complete
		e2eClickButton(t, page, "Complete")
		e2eWaitForStatus(t, page, "completed")

		// No action buttons should be visible (terminal state)
		// Claim, Release, Complete, Respond should all be gone
		for _, btn := range []string{"Claim", "Release", "Complete", "Respond"} {
			loc := page.Locator("#drawer-panel button:has-text('" + btn + "')")
			visible, err := loc.IsVisible()
			if err != nil {
				t.Fatalf("check %s button: %v", btn, err)
			}
			if visible {
				t.Errorf("%s button should not be visible in completed state", btn)
			}
		}
	})
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 -timeout 120s -run TestE2E/RespondComplete ./cmd/inboxui/
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E RespondComplete subtest"
```

---

### Task 9: Cancel subtest

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Replace the Cancel placeholder subtest**

```go
	t.Run("Cancel", func(t *testing.T) {
		page := e2eNewPage(t, browser, baseURL+"/inbox")

		e2eClickItem(t, page, e2eItemTitles[1]) // "Approve Vendor Contract"
		e2eAssertStatus(t, page, "open")

		// Click Cancel to toggle open the cancel form
		e2eClickButton(t, page, "Cancel")

		// Fill reason textarea
		reasonInput := page.Locator("textarea[placeholder='Reason for cancellation']")
		if err := reasonInput.WaitFor(); err != nil {
			t.Fatalf("cancel form not visible: %v", err)
		}
		if err := reasonInput.Fill("No longer needed"); err != nil {
			t.Fatalf("fill cancel reason: %v", err)
		}

		// Click Confirm Cancel
		e2eClickButton(t, page, "Confirm Cancel")
		e2eWaitForStatus(t, page, "cancelled")

		// No action buttons in terminal state
		for _, btn := range []string{"Claim", "Cancel", "Release", "Complete"} {
			loc := page.Locator("#drawer-panel button:has-text('" + btn + "')")
			visible, err := loc.IsVisible()
			if err != nil {
				t.Fatalf("check %s button: %v", btn, err)
			}
			if visible {
				t.Errorf("%s button should not be visible in cancelled state", btn)
			}
		}
	})
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 -timeout 120s -run TestE2E/Cancel ./cmd/inboxui/
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E Cancel subtest"
```

---

### Task 10: Comment subtest

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Replace the Comment placeholder subtest**

```go
	t.Run("Comment", func(t *testing.T) {
		page := e2eNewPage(t, browser, baseURL+"/inbox")

		e2eClickItem(t, page, e2eItemTitles[3]) // "Check Consent Records"

		// Fill comment textarea
		commentInput := page.Locator("textarea[placeholder='Write a comment...']")
		if err := commentInput.Fill("Test comment from E2E"); err != nil {
			t.Fatalf("fill comment: %v", err)
		}

		// Click Comment button
		commentBtn := page.Locator("#drawer-panel button:has-text('Comment')").Last()
		if err := commentBtn.Click(); err != nil {
			t.Fatalf("click Comment button: %v", err)
		}

		// Wait for "Comment" event label in the activity feed
		commentEvent := page.Locator("#drawer-panel .font-medium.text-sm:has-text('Comment')")
		if err := commentEvent.WaitFor(); err != nil {
			t.Fatalf("Comment event not visible: %v", err)
		}

		// Verify the comment body text appears
		commentBody := page.Locator("#drawer-panel:has-text('Test comment from E2E')")
		visible, err := commentBody.IsVisible()
		if err != nil {
			t.Fatalf("check comment body: %v", err)
		}
		if !visible {
			t.Error("comment body 'Test comment from E2E' not visible in activity feed")
		}

		// Verify actor is "e2e-tester" (actorDisplayName("user:e2e-tester"))
		actorLabel := page.Locator("#drawer-panel:has-text('e2e-tester')")
		visible, err = actorLabel.IsVisible()
		if err != nil {
			t.Fatalf("check actor: %v", err)
		}
		if !visible {
			t.Error("actor 'e2e-tester' not visible in activity feed")
		}
	})
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 -timeout 120s -run TestE2E/Comment ./cmd/inboxui/
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E Comment subtest"
```

---

### Task 11: Search subtest

**Files:**
- Modify: `cmd/inboxui/e2e_test.go`

- [ ] **Step 1: Replace the Search placeholder subtest**

The search handler reads `r.URL.Query().Get("q")` directly, so we navigate with `?q=` for reliability (see spec for details on Datastar signal encoding mismatch).

```go
	t.Run("Search", func(t *testing.T) {
		// Navigate directly with ?q= query param (see spec note on Datastar signal encoding)
		page := e2eNewPage(t, browser, baseURL+"/inbox/search?q=Refund")

		// "Process Refund Request" should appear in results
		resultRow := page.Locator("#queue-table tr:has-text('" + e2eItemTitles[4] + "')")
		if err := resultRow.WaitFor(); err != nil {
			t.Fatalf("search result not visible: %v", err)
		}

		// Other items should not appear
		for _, title := range []string{e2eItemTitles[0], e2eItemTitles[3]} {
			otherRow := page.Locator("#queue-table tr:has-text('" + title + "')")
			visible, err := otherRow.IsVisible()
			if err != nil {
				t.Fatalf("check non-result %q: %v", title, err)
			}
			if visible {
				t.Errorf("item %q should not appear in search results for 'Refund'", title)
			}
		}
	})
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 -timeout 120s -run TestE2E/Search ./cmd/inboxui/
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: add E2E Search subtest"
```

---

### Task 12: Run all E2E subtests together

**Files:**
- No changes

- [ ] **Step 1: Run full TestE2E**

```bash
go test -v -count=1 -timeout 180s -run TestE2E ./cmd/inboxui/
```

Expected: all 8 subtests PASS. If any fail due to state ordering issues (subtests mutate shared data), debug by checking which prior subtest left the item in an unexpected state. The subtests are designed to use different items to avoid conflicts:

| Subtest | Item used | Final state |
|---------|-----------|-------------|
| Queue | all 5 | no mutation |
| Detail | Process Refund Request | no mutation |
| ClaimRelease | Review Q1 Report | open (released) |
| MyWork | Review Q1 Report | claimed |
| RespondComplete | Verify Customer Address | completed |
| Cancel | Approve Vendor Contract | cancelled |
| Comment | Check Consent Records | open (just commented) |
| Search | reads all | no mutation |

Note: After MyWork, "Review Q1 Report" stays claimed. The Queue subtest runs first (all open), so this is fine.

- [ ] **Step 2: Commit (final cleanup if needed)**

```bash
git add cmd/inboxui/e2e_test.go
git commit -m "test: finalize E2E Playwright tests — all 8 subtests passing"
```
