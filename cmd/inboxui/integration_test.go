package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/laenen-partners/entitystore"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	schemav1 "github.com/laenen-partners/inbox/cmd/inboxui/schema/gen/schema/v1"
	"github.com/laenen-partners/inbox/gen/inbox/v1/inboxv1connect"
	"github.com/laenen-partners/inbox/service"
	inboxui "github.com/laenen-partners/inbox/ui"
	"github.com/laenen-partners/tags"
)

var _sharedConnStr string

func testInbox(t *testing.T) *inbox.Inbox {
	t.Helper()
	ctx := context.Background()

	if _sharedConnStr == "" {
		pg, err := postgres.Run(ctx,
			"pgvector/pgvector:pg17",
			postgres.WithDatabase("inbox_ui_test"),
			postgres.WithUsername("test"),
			postgres.WithPassword("test"),
			postgres.BasicWaitStrategies(),
		)
		if err != nil {
			t.Fatalf("start postgres container: %v", err)
		}

		connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("get connection string: %v", err)
		}

		pool, err := pgxpool.New(ctx, connStr)
		if err != nil {
			t.Fatalf("create pool for migration: %v", err)
		}
		if err := store.Migrate(ctx, pool); err != nil {
			pool.Close()
			t.Fatalf("migrate: %v", err)
		}
		pool.Close()

		_sharedConnStr = connStr
	}

	pool, err := pgxpool.New(ctx, _sharedConnStr)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	es, err := entitystore.New(entitystore.WithPgStore(pool))
	if err != nil {
		t.Fatalf("create entity store: %v", err)
	}

	return inbox.New(es)
}

func testClient(ib *inbox.Inbox) inboxv1connect.InboxServiceClient {
	return service.NewLocalClient(service.NewHandler(ib))
}

func testCtx() context.Context {
	id, _ := identity.New("test", "test", "test", identity.PrincipalUser, nil)
	return identity.WithContext(context.Background(), id)
}

func TestFilterDropdowns(t *testing.T) {
	ib := testInbox(t)
	ctx := testCtx()

	handler := inboxui.Handler(testClient(ib),
		inboxui.WithIdentity(func(r *http.Request) identity.Context {
			id, _ := identity.New("test", "test", "test", identity.PrincipalUser, nil)
			return id
		}),
		inboxui.WithFilter(inboxui.FilterConfig{
			Label: "Priority", TagPrefix: "priority:",
			Options: []string{"urgent", "high", "normal", "low"},
		}),
		inboxui.WithFilter(inboxui.FilterConfig{
			Label: "Team", TagPrefix: "team:",
			Options: []string{"compliance", "ops", "finance"},
		}),
	)

	// Seed items with different priorities
	for _, p := range []string{"urgent", "normal"} {
		_, err := ib.Create(ctx, inbox.Meta{
			Title: "Item " + p,
			Tags:  tags.MustNew("type:review", "priority:"+p, tags.Team("compliance")),
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	t.Run("full_page_no_filter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		body := rec.Body.String()
		t.Logf("Status: %d, Body len: %d", rec.Code, len(body))
		if !strings.Contains(body, "Item urgent") {
			t.Error("missing 'Item urgent'")
		}
		if !strings.Contains(body, "Item normal") {
			t.Error("missing 'Item normal'")
		}
		// Check that data-signals and data-bind are rendered
		if !strings.Contains(body, "data-signals") {
			t.Error("missing data-signals attribute on filter container")
		}
		if !strings.Contains(body, "data-bind") {
			t.Error("missing data-bind attribute on select")
		}
		t.Logf("data-signals present: %v", strings.Contains(body, "data-signals"))
		t.Logf("data-bind present: %v", strings.Contains(body, "data-bind"))

		// Log the actual filter HTML for debugging
		idx := strings.Index(body, "queue-filters")
		if idx > 0 {
			end := idx + 500
			if end > len(body) {
				end = len(body)
			}
			t.Logf("Filter HTML: ...%s...", body[idx:end])
		}
	})

	t.Run("sse_with_priority_filter", func(t *testing.T) {
		// Datastar sends signals as ?datastar={"priority":"urgent","team":""}
		dsParam := `{"priority":"urgent","team":""}`
		req := httptest.NewRequest("GET", "/?datastar="+dsParam, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		body := rec.Body.String()
		t.Logf("SSE Status: %d, Content-Type: %s", rec.Code, rec.Header().Get("Content-Type"))
		t.Logf("SSE Body:\n%s", body)

		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(body, "Item urgent") {
			t.Error("SSE response missing 'Item urgent'")
		}
		if strings.Contains(body, "Item normal") {
			t.Error("SSE response should NOT contain 'Item normal' (filtered out)")
		}
	})

	t.Run("sse_with_team_filter", func(t *testing.T) {
		dsParam := `{"priority":"","team":"finance"}`
		req := httptest.NewRequest("GET", "/?datastar="+dsParam, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		body := rec.Body.String()
		t.Logf("SSE Body:\n%s", body)

		if strings.Contains(body, "Item urgent") || strings.Contains(body, "Item normal") {
			t.Error("SSE response should have no items for team:finance")
		}
		if !strings.Contains(body, "Queue is empty") {
			t.Error("SSE response missing empty state")
		}
	})
}

func TestSchemaRendererIntegration(t *testing.T) {
	ib := testInbox(t)
	ctx := testCtx()

	handler := inboxui.Handler(testClient(ib),
		inboxui.WithIdentity(func(r *http.Request) identity.Context {
			id, _ := identity.New("test", "test", "test", identity.PrincipalUser, nil)
			return id
		}),
		inboxui.WithContentProvider("schema.v1.ItemSchema", schemaProvider{}),
		inboxui.WithContentProvider("inbox.v1.ItemSchema", schemaProvider{}),
	)

	t.Run("display_fields", func(t *testing.T) {
		item, err := ib.Create(ctx, inbox.Meta{
			Title: "Schema display test",
			Tags:  tags.MustNew("type:review"),
			Payload: &schemav1.ItemSchema{
				Display: []*schemav1.DisplayField{
					{Label: "Customer", Value: "CUST-1234"},
					{Label: "Transaction", Value: "TXN-9999", Mono: true},
				},
			},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()

		// Schema ContentProvider renders display fields (labels and values).
		for _, want := range []string{"Customer", "CUST-1234", "Transaction", "TXN-9999", "font-mono"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %q in response", want)
			}
		}
	})

	t.Run("form_fields", func(t *testing.T) {
		item, _ := ib.Create(ctx, inbox.Meta{
			Title: "Schema form test",
			Tags:  tags.MustNew("type:input_required"),
			Payload: &schemav1.ItemSchema{
				Fields: []*schemav1.FormField{
					{Name: "name", Type: "text", Label: "Full Name", Placeholder: "John Doe", Required: true},
					{Name: "notes", Type: "textarea", Label: "Notes"},
					{Name: "country", Type: "select", Label: "Country", Options: []string{"NL", "BE", "DE"}},
					{Name: "agree", Type: "checkbox", Label: "I agree to terms"},
				},
			},
		})

		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()

		// Schema ContentProvider renders all form field types.
		for _, want := range []string{"Full Name", "John Doe", "textarea", "Country", "NL", "checkbox", "I agree"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %q in response", want)
			}
		}
	})

	t.Run("submit_button_when_claimed_and_completable", func(t *testing.T) {
		// Submit button appears when item is claimed by actor and ClientCompletable.
		item, _ := ib.Create(ctx, inbox.Meta{
			Title: "Submit button test",
			Tags:  tags.MustNew("type:approval"),
			Payload: &schemav1.ItemSchema{
				ClientCompletable: true,
			},
		})

		// Claim the item so the submit button appears.
		_, _ = ib.Claim(ctx, item.ID)

		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()

		if !strings.Contains(body, "Submit") {
			t.Error("missing Submit button for claimed + ClientCompletable item")
		}
		if !strings.Contains(body, "/close") {
			t.Error("missing /close URL in button")
		}
	})

	t.Run("shell_buttons_open_item", func(t *testing.T) {
		// For an open item the shell renders Claim; the schema
		// ContentProvider does not inject those shell-level buttons.
		item, err := ib.Create(ctx, inbox.Meta{
			Title: "Shell buttons test",
			Tags:  tags.MustNew("type:review"),
			Payload: &schemav1.ItemSchema{
				Display: []*schemav1.DisplayField{
					{Label: "Ref", Value: "REF-001"},
				},
			},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()

		// Shell must contain Claim for an open item.
		if !strings.Contains(body, "Claim") {
			t.Error("missing Claim button for open item")
		}
		// Ref content from provider must also be present.
		if !strings.Contains(body, "REF-001") {
			t.Error("missing provider-rendered display content")
		}
	})
}

// TestClaimReleaseCloseFlow verifies the end-to-end lifecycle:
// create → view (Claim shown) → claim → view (Release shown) → close.
func TestClaimReleaseCloseFlow(t *testing.T) {
	ib := testInbox(t)
	ctx := testCtx()

	// Use actor "user:test" — matches identity principal "test:test" in testCtx.
	actor := "user:test"

	handler := inboxui.Handler(testClient(ib),
		inboxui.WithIdentity(func(r *http.Request) identity.Context {
			id, _ := identity.New("test", "test", "test", identity.PrincipalUser, nil)
			return id
		}),
		inboxui.WithContentProvider("schema.v1.ItemSchema", schemaProvider{}),
		inboxui.WithContentProvider("inbox.v1.ItemSchema", schemaProvider{}),
	)

	item, err := ib.Create(ctx, inbox.Meta{
		Title: "Lifecycle flow test",
		Tags:  tags.MustNew("type:approval"),
		Payload: &schemav1.ItemSchema{
			ClientCompletable: true,
		},
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	// Step 1: open item → Claim button in shell, Submit visible (ClientCompletable).
	{
		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()
		if !strings.Contains(body, "Claim") {
			t.Error("open item: expected Claim button in shell")
		}
		if !strings.Contains(body, "Submit") {
			t.Error("open item: expected Submit button for ClientCompletable item")
		}
	}

	// Step 2: claim the item via the POST endpoint.
	{
		req := httptest.NewRequest("POST", "/items/"+item.ID+"/claim", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("claim: expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Item claimed") {
			t.Error("claim: expected toast 'Item claimed'")
		}
	}

	// Step 3: tag the assignee (mirrors what handleClaim does) so IsClaimant is true.
	if err := ib.Tag(ctx, item.ID, "assignee:"+actor); err != nil {
		t.Fatalf("tag assignee: %v", err)
	}

	// Step 4: claimed item → Release in shell; Submit button from provider.
	{
		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()
		if !strings.Contains(body, "Release") {
			t.Error("claimed item: expected Release button in shell")
		}
		if !strings.Contains(body, "Submit") {
			t.Error("claimed item: expected Submit button from provider")
		}
	}

	// Step 5: close the item directly via the inbox API.
	{
		if _, err := ib.Close(ctx, item.ID, "approved"); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	// Step 6: closed item → no action buttons in shell.
	{
		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()
		if strings.Contains(body, "btn-neutral\">Claim") || strings.Contains(body, "btn-ghost\">Release") {
			t.Error("closed item: Claim/Release must not appear")
		}
		if !strings.Contains(body, "closed") {
			t.Error("closed item: expected status 'closed'")
		}
	}
}
