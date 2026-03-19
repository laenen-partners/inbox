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
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	inboxui "github.com/laenen-partners/inbox/ui"
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

func TestFilterDropdowns(t *testing.T) {
	ib := testInbox(t)
	ctx := context.Background()

	handler := inboxui.Handler(ib,
		inboxui.WithActor(func(r *http.Request) string { return "test" }),
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
			Actor: "test",
			Tags:  []string{"type:review", "priority:" + p, "team:compliance"},
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
		if !strings.Contains(body, "No items found") {
			t.Error("SSE response missing 'No items found' empty state")
		}
	})
}

func TestSchemaRendererIntegration(t *testing.T) {
	ib := testInbox(t)
	ctx := context.Background()

	handler := inboxui.Handler(ib,
		inboxui.WithPayloadRenderer("inbox.v1.ItemSchema", inboxui.SchemaRenderer()),
		inboxui.WithActor(func(r *http.Request) string { return "test" }),
	)

	t.Run("display_fields", func(t *testing.T) {
		item, err := ib.Create(ctx, inbox.Meta{
			Title: "Schema display test", Actor: "test",
			Tags: []string{"type:review"},
			Payload: &inboxv1.ItemSchema{
				Display: []*inboxv1.DisplayField{
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

		for _, want := range []string{"Customer", "CUST-1234", "Transaction", "TXN-9999", "font-mono"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %q in response", want)
			}
		}
	})

	t.Run("form_fields", func(t *testing.T) {
		item, _ := ib.Create(ctx, inbox.Meta{
			Title: "Schema form test", Actor: "test",
			Tags: []string{"type:input_required"},
			Payload: &inboxv1.ItemSchema{
				Fields: []*inboxv1.FormField{
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

		for _, want := range []string{"Full Name", "John Doe", "textarea", "Country", "NL", "checkbox", "I agree"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %q in response", want)
			}
		}
	})

	t.Run("actions", func(t *testing.T) {
		item, _ := ib.Create(ctx, inbox.Meta{
			Title: "Schema actions test", Actor: "test",
			Tags: []string{"type:approval"},
			Payload: &inboxv1.ItemSchema{
				Actions: []*inboxv1.Action{
					{Name: "approve", Label: "Approve", Variant: "success"},
					{Name: "reject", Label: "Reject", Variant: "error"},
				},
			},
		})

		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()

		for _, want := range []string{"Approve", "Reject", "badge-success", "badge-error"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %q in response", want)
			}
		}
	})
}

