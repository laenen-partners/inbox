package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/laenen-partners/entitystore"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/inbox"
	inboxui "github.com/laenen-partners/inbox/ui"
	"google.golang.org/protobuf/types/known/structpb"
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

func TestPayloadRendererIntegration(t *testing.T) {
	ib := testInbox(t)
	ctx := context.Background()

	handler := inboxui.Handler(ib,
		inboxui.WithPayloadRenderer("google.protobuf.Struct", structRenderer),
		inboxui.WithActor(func(r *http.Request) string { return "test" }),
	)

	// Helper to create item, GET detail via handler, assert HTML contains expected strings
	assertPayloadRenders := func(t *testing.T, meta inbox.Meta, wantInHTML []string) {
		t.Helper()

		item, err := ib.Create(ctx, meta)
		if err != nil {
			t.Fatalf("create item: %v", err)
		}

		// Verify payload survives entity store roundtrip
		got, err := ib.Get(ctx, item.ID)
		if err != nil {
			t.Fatalf("get item: %v", err)
		}
		t.Logf("PayloadType=%q, Payload nil=%v", got.PayloadType(), got.Proto.GetPayload() == nil)
		if got.Proto.GetPayload() != nil {
			t.Logf("Payload TypeUrl=%q Value.len=%d", got.Proto.GetPayload().GetTypeUrl(), len(got.Proto.GetPayload().GetValue()))
		}

		// Test renderer directly with stored data
		if got.Proto.GetPayload() != nil {
			component := structRenderer(got.PayloadType(), got.Proto.GetPayload().GetValue())
			if component == nil {
				t.Fatalf("structRenderer returned nil for stored payload (bytes: %x)", got.Proto.GetPayload().GetValue())
			}
		}

		// Test via HTTP
		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		resp := rec.Result()
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		if resp.StatusCode != 200 {
			t.Fatalf("HTTP %d: %s", resp.StatusCode, bodyStr)
		}

		for _, want := range wantInHTML {
			if !strings.Contains(bodyStr, want) {
				t.Errorf("HTTP response missing %q\nBody:\n%s", want, bodyStr)
			}
		}
	}

	t.Run("address_request_empty", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"_type":   "address_request",
			"message": "Provide business address",
			"street":  "",
			"city":    "",
			"zip":     "",
			"country": "",
		})
		assertPayloadRenders(t, inbox.Meta{
			Title: "Empty address", Actor: "test", Payload: payload,
			Tags: []string{"type:input_required"},
		}, []string{"Address Required", "Not provided"})
	})

	t.Run("address_request_prefilled", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"_type":   "address_request",
			"message": "Verify billing address",
			"street":  "123 Main St",
			"city":    "Amsterdam",
			"zip":     "1012 AB",
			"country": "Netherlands",
		})
		assertPayloadRenders(t, inbox.Meta{
			Title: "Prefilled address", Actor: "test", Payload: payload,
			Tags: []string{"type:approval"},
		}, []string{"Address Required", "Amsterdam", "1012 AB", "Netherlands"})
	})

	t.Run("consent_request", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"_type": "consent_request",
			"items": []interface{}{
				map[string]interface{}{
					"name":        "Data Processing Agreement",
					"description": "GDPR consent",
					"required":    true,
				},
				map[string]interface{}{
					"name":        "Marketing Communications",
					"description": "Opt-in emails",
					"required":    false,
				},
			},
		})
		assertPayloadRenders(t, inbox.Meta{
			Title: "Consent review", Actor: "test", Payload: payload,
			Tags: []string{"type:approval"},
		}, []string{"Consent Review", "Data Processing Agreement", "Marketing Communications", "required"})
	})

	t.Run("multi_choice_single", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"_type":          "multi_choice",
			"question":       "Select payment terms",
			"allow_multiple": false,
			"options":        []interface{}{"Net 30", "Net 60", "Net 90"},
			"note":           "Customer requested Net 60",
		})
		assertPayloadRenders(t, inbox.Meta{
			Title: "Payment terms", Actor: "test", Payload: payload,
			Tags: []string{"type:review"},
		}, []string{"Single Choice", "Net 30", "Net 60", "radio", "Customer requested Net 60"})
	})

	t.Run("multi_choice_multi", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"_type":          "multi_choice",
			"question":       "Documents provided",
			"allow_multiple": true,
			"options":        []interface{}{"Government ID", "Proof of address", "Bank statement"},
		})
		assertPayloadRenders(t, inbox.Meta{
			Title: "Document check", Actor: "test", Payload: payload,
			Tags: []string{"type:review"},
		}, []string{"Multi-Select", "Government ID", "checkbox"})
	})

	t.Run("generic_falls_back_to_json", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"amount": 2450.00,
			"note":   "expense data",
		})
		item, err := ib.Create(ctx, inbox.Meta{
			Title: "Generic payload", Actor: "test", Payload: payload,
			Tags: []string{"type:approval"},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// structRenderer should return nil (no _type field)
		got, _ := ib.Get(ctx, item.ID)
		component := structRenderer(got.PayloadType(), got.Proto.GetPayload().GetValue())
		if component != nil {
			t.Error("expected nil for generic struct, got component")
		}

		// HTTP response should still have Payload section (json fallback)
		req := httptest.NewRequest("GET", "/items/"+item.ID, nil)
		req.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		body, _ := io.ReadAll(rec.Result().Body)
		if !strings.Contains(string(body), "Payload") {
			t.Error("response missing Payload section")
		}
	})

	t.Run("queue_page_lists_items", func(t *testing.T) {
		// Full page render (not SSE)
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		body, _ := io.ReadAll(rec.Result().Body)
		bodyStr := string(body)

		if rec.Result().StatusCode != 200 {
			t.Fatalf("queue page returned %d", rec.Result().StatusCode)
		}
		// Should contain at least one of our created items
		if !strings.Contains(bodyStr, "Empty address") && !strings.Contains(bodyStr, "Prefilled address") {
			t.Errorf("queue page missing created items\nBody length: %d", len(bodyStr))
		}
	})
}
