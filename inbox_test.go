package inbox_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/laenen-partners/entitystore"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
)

// ─── Test infrastructure ───

var _sharedConnStr string

func sharedInbox(t *testing.T) *inbox.Inbox {
	t.Helper()
	ctx := context.Background()

	if _sharedConnStr == "" {
		pg, err := postgres.Run(ctx,
			"pgvector/pgvector:pg17",
			postgres.WithDatabase("inbox_test"),
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

// ─── Payload helpers ───

// KYC review payload — simulates what prodcat would create.
type eligibilityReviewPayload struct {
	SubscriptionID  string `json:"subscription_id"`
	ProductID       string `json:"product_id"`
	ProductName     string `json:"product_name"`
	RequirementName string `json:"requirement_name"`
	Category        string `json:"category"`
	FailureMode     string `json:"failure_mode"`
	ResolutionHint  string `json:"resolution_hint"`
	CustomerID      string `json:"customer_id"`
	PartyID         string `json:"party_id"`
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// ─── E2E Tests: KYC & Onboarding Scenarios ───

// TestCustomerMissingDocuments simulates a customer who starts onboarding
// but hasn't uploaded their identity document yet. The eligibility engine
// creates an inbox item asking the customer to upload. The customer
// eventually provides the document and responds.
func TestCustomerMissingDocuments(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	// 1. Workflow evaluates eligibility → ID document is missing.
	//    Creates an inbox item for the customer.
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Upload your identity document",
		Description: "To continue opening your Current Account, please upload a valid passport or Emirates ID.",
		PayloadType: "eligibility.v1.DocumentUploadRequest",
		Payload: mustJSON(t, map[string]any{
			"@type":           "type.googleapis.com/eligibility.v1.DocumentUploadRequest",
			"subscription_id": "SUB-2026-0042",
			"product_name":    "Current Account — AED",
			"accepted_types":  []string{"passport", "emirates_id"},
			"customer_id":     "CUST-1234",
		}),
		Actor: "workflow:onboarding-456",
		Tags: []string{
			"type:input_required",
			"assignee:customer:CUST-1234",
			"workflow:onboarding-456",
			"ref:subscription:SUB-2026-0042",
			"priority:normal",
		},
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	if item.Status != inbox.StatusOpen {
		t.Errorf("expected status open, got %s", item.Status)
	}
	if item.PayloadType != "eligibility.v1.DocumentUploadRequest" {
		t.Errorf("expected payload type, got %s", item.PayloadType)
	}
	if !inbox.HasTag(item, "type:input_required") {
		t.Error("expected type:input_required tag")
	}

	// 2. Customer queries their inbox — finds the open item.
	items, err := ib.ListByTags(ctx, []string{"assignee:customer:CUST-1234", "status:open"}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one item for customer")
	}

	// 3. Customer uploads document and responds.
	item, err = ib.Respond(ctx, item.ID, inbox.Response{
		Actor:  "user:customer:CUST-1234",
		Action: "submit",
		Data: mustJSON(t, map[string]any{
			"document_type":  "passport",
			"document_ref":   "DOC-789",
			"issuing_country": "AE",
		}),
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	// Status is still open — workflow decides when to complete.
	if item.Status != inbox.StatusOpen {
		t.Errorf("expected status open after respond, got %s", item.Status)
	}

	// 4. Verify event log records everything.
	if len(item.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(item.Events))
	}
	if item.Events[0].Type != "inbox.v1.ItemCreated" {
		t.Errorf("expected ItemCreated event, got %s", item.Events[0].Type)
	}
	if item.Events[1].Type != "inbox.v1.ItemResponded" {
		t.Errorf("expected ItemResponded event, got %s", item.Events[1].Type)
	}
	if item.Events[1].Actor != "user:customer:CUST-1234" {
		t.Errorf("expected customer actor, got %s", item.Events[1].Actor)
	}

	// 5. Workflow re-evaluates and completes the item.
	item, err = ib.Complete(ctx, item.ID, "workflow:onboarding-456")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if item.Status != inbox.StatusCompleted {
		t.Errorf("expected completed, got %s", item.Status)
	}
	if !inbox.HasTag(item, "status:completed") {
		t.Error("expected status:completed tag")
	}
}

// TestComplianceReviewPEPScreening simulates a PEP (Politically Exposed
// Person) screening that flags a customer during onboarding. A compliance
// officer must review and make a decision.
func TestComplianceReviewPEPScreening(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	// 1. Eligibility engine flags PEP screening → creates compliance review item.
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "PEP screening review — Ahmed K.",
		Description: "Customer flagged as Politically Exposed Person during onboarding for Current Account — AED. Review screening report and decide.",
		PayloadType: "eligibility.v1.EligibilityReviewPayload",
		Payload: mustJSON(t, eligibilityReviewPayload{
			SubscriptionID:  "SUB-2026-0042",
			ProductID:       "casa-aed",
			ProductName:     "Current Account — AED",
			RequirementName: "pep_screening_clear",
			Category:        "kyc",
			FailureMode:     "manual_review",
			ResolutionHint:  "Review PEP screening report. Approve if risk is acceptable, reject if not.",
			CustomerID:      "CUST-1234",
			PartyID:         "PARTY-5678",
		}),
		Actor: "workflow:onboarding-456",
		Tags: []string{
			"type:review",
			"team:compliance",
			"priority:high",
			"assignee:team:compliance",
			"ref:subscription:SUB-2026-0042",
			"ref:customer:CUST-1234",
			"workflow:onboarding-456",
		},
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	// 2. Compliance officer finds items needing review.
	items, err := ib.ListByTags(ctx, []string{"team:compliance", "status:open"}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	found := false
	for _, it := range items {
		if it.ID == item.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("compliance review item not found in team queue")
	}

	// 3. Compliance officer claims the item.
	item, err = ib.Claim(ctx, item.ID, "user:compliance:fatima")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if item.Status != inbox.StatusClaimed {
		t.Errorf("expected claimed, got %s", item.Status)
	}

	// 4. Compliance officer adds an internal note.
	item, err = ib.Comment(ctx, item.ID, "user:compliance:fatima",
		"Checked screening report. PEP status is historical — former deputy minister, left office 2019. Low risk.",
		&inbox.CommentOpts{Visibility: []string{"team:compliance"}},
	)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}

	// 5. Compliance officer approves.
	item, err = ib.Respond(ctx, item.ID, inbox.Response{
		Actor:  "user:compliance:fatima",
		Action: "approve",
		Data: mustJSON(t, map[string]any{
			"risk_assessment": "low",
			"justification":   "Historical PEP, no current political exposure. Low risk.",
		}),
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	// 6. Workflow completes.
	item, err = ib.Complete(ctx, item.ID, "workflow:onboarding-456")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	// 7. Verify full event trail.
	if len(item.Events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(item.Events))
	}

	expectedTypes := []string{"inbox.v1.ItemCreated", "inbox.v1.ItemClaimed", "inbox.v1.CommentAppended", "inbox.v1.ItemResponded", "inbox.v1.ItemCompleted"}
	for i, typ := range expectedTypes {
		if item.Events[i].Type != typ {
			t.Errorf("event %d: expected %s, got %s", i, typ, item.Events[i].Type)
		}
	}

	// Verify typed event data.
	if item.Events[1].Type != "inbox.v1.ItemClaimed" {
		t.Errorf("expected ItemClaimed, got %s", item.Events[1].Type)
	}
	if item.Events[2].Type != "inbox.v1.CommentAppended" {
		t.Errorf("expected CommentAppended, got %s", item.Events[2].Type)
	}
}

// TestRMOverrideOnBehalfOfClient simulates a relationship manager
// completing an action on behalf of a client who called the bank.
// The RM responds to the client's inbox item, and the event trail
// captures the delegation.
func TestRMOverrideOnBehalfOfClient(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	// 1. Workflow creates item for customer to accept terms.
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Accept Terms & Conditions",
		Description: "Please review and accept the General Terms & Conditions to continue your account opening.",
		PayloadType: "eligibility.v1.AgreementRequest",
		Payload: mustJSON(t, map[string]any{
			"@type":           "type.googleapis.com/eligibility.v1.AgreementRequest",
			"agreement_type":  "general_terms_and_conditions",
			"agreement_version": "2026.1",
			"subscription_id": "SUB-2026-0099",
		}),
		Actor: "workflow:onboarding-789",
		Tags: []string{
			"type:action",
			"assignee:customer:CUST-5678",
			"rm:user:sarah",
			"workflow:onboarding-789",
			"priority:normal",
		},
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	// 2. Customer doesn't respond for a while. RM sees it in their view.
	items, err := ib.ListByTags(ctx, []string{"rm:user:sarah", "status:open"}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected item in RM's view")
	}

	// 3. Customer calls the bank. RM claims and acts on their behalf.
	item, err = ib.Claim(ctx, item.ID, "user:rm:sarah")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	item, err = ib.Comment(ctx, item.ID, "user:rm:sarah",
		"Client called branch. Read T&C over the phone, client verbally accepted.",
		nil,
	)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}

	// 4. RM responds on behalf of the client.
	item, err = ib.Respond(ctx, item.ID, inbox.Response{
		Actor:  "user:rm:sarah",
		Action: "accept",
		Data: mustJSON(t, map[string]any{
			"on_behalf_of":    "customer:CUST-5678",
			"override_reason": "Client verbally accepted T&C during phone call. Branch override.",
			"accepted":        true,
		}),
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	// 5. Workflow completes.
	item, err = ib.Complete(ctx, item.ID, "workflow:onboarding-789")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	// 6. Verify the override is captured in the event trail.
	var respondEvt inbox.Event
	for _, evt := range item.Events {
		if evt.Type == "inbox.v1.ItemResponded" {
			respondEvt = evt
		}
	}
	if respondEvt.Actor != "user:rm:sarah" {
		t.Errorf("expected RM as actor, got %s", respondEvt.Actor)
	}

	// The response data contains the on_behalf_of field.
	var respData map[string]any
	if err := json.Unmarshal(respondEvt.Data, &respData); err != nil {
		t.Fatalf("unmarshal response data: %v", err)
	}
	if respData["on_behalf_of"] != "customer:CUST-5678" {
		t.Errorf("expected on_behalf_of customer, got %v", respData["on_behalf_of"])
	}
	if respData["override_reason"] == nil {
		t.Error("expected override_reason in response data")
	}
}

// TestEscalationFromOpsToCompliance simulates an ops team member
// escalating an item to compliance when they discover it needs
// specialist review.
func TestEscalationFromOpsToCompliance(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	// 1. Item created for ops team.
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Source of funds verification — Large deposit",
		Description: "Customer deposited $250,000. Source of funds documentation required per AML policy.",
		PayloadType: "eligibility.v1.EligibilityReviewPayload",
		Payload: mustJSON(t, eligibilityReviewPayload{
			SubscriptionID:  "SUB-2026-0150",
			ProductID:       "casa-aed",
			ProductName:     "Current Account — AED",
			RequirementName: "source_of_funds_verified",
			Category:        "kyc",
			FailureMode:     "manual_review",
			ResolutionHint:  "Review source of funds documentation.",
			CustomerID:      "CUST-9999",
			PartyID:         "PARTY-9999",
		}),
		Actor: "workflow:monitoring-100",
		Tags: []string{
			"type:review",
			"team:ops",
			"priority:high",
			"ref:customer:CUST-9999",
		},
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	// 2. Ops team member claims and reviews.
	item, err = ib.Claim(ctx, item.ID, "user:ops:marco")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	item, err = ib.Comment(ctx, item.ID, "user:ops:marco",
		"Amount exceeds $100k threshold. Customer is a PEP. Escalating to compliance for enhanced due diligence.",
		nil,
	)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}

	// 3. Escalate to compliance.
	item, err = ib.Escalate(ctx, item.ID, "user:ops:marco", "ops", "compliance",
		"Amount exceeds threshold and customer is flagged PEP. Needs enhanced due diligence.")
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}

	// 4. Verify team tag was updated.
	item, err = ib.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if inbox.HasTag(item, "team:ops") {
		t.Error("expected team:ops tag to be removed")
	}
	if !inbox.HasTag(item, "team:compliance") {
		t.Error("expected team:compliance tag")
	}

	// 5. Verify escalation event.
	var escalationEvt inbox.Event
	for _, evt := range item.Events {
		if evt.Type == "inbox.v1.ItemEscalated" {
			escalationEvt = evt
		}
	}
	if escalationEvt.Type != "inbox.v1.ItemEscalated" {
		t.Errorf("expected ItemEscalated, got %s", escalationEvt.Type)
	}

	var escData inboxv1.ItemEscalated
	if err := json.Unmarshal(escalationEvt.Data, &escData); err != nil {
		t.Fatalf("unmarshal escalation: %v", err)
	}
	if escData.FromTeam != "ops" || escData.ToTeam != "compliance" {
		t.Errorf("expected ops→compliance, got %s→%s", escData.FromTeam, escData.ToTeam)
	}

	// 6. Compliance picks up and resolves.
	// Release first (ops had it claimed), then compliance claims.
	item, err = ib.Release(ctx, item.ID, "user:ops:marco")
	if err != nil {
		t.Fatalf("release: %v", err)
	}

	item, err = ib.Claim(ctx, item.ID, "user:compliance:fatima")
	if err != nil {
		t.Fatalf("claim by compliance: %v", err)
	}

	item, err = ib.Respond(ctx, item.ID, inbox.Response{
		Actor:   "user:compliance:fatima",
		Action:  "approve",
		Comment: "EDD completed. Source of funds verified via bank statements and employment letter.",
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	item, err = ib.Complete(ctx, item.ID, "workflow:monitoring-100")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	// 7. Full event trail.
	if len(item.Events) != 8 {
		t.Fatalf("expected 8 events, got %d", len(item.Events))
	}
	expectedTypes := []string{
		"inbox.v1.ItemCreated", "inbox.v1.ItemClaimed", "inbox.v1.CommentAppended", "inbox.v1.ItemEscalated",
		"inbox.v1.ItemReleased", "inbox.v1.ItemClaimed", "inbox.v1.ItemResponded", "inbox.v1.ItemCompleted",
	}
	for i, typ := range expectedTypes {
		if item.Events[i].Type != typ {
			t.Errorf("event %d: expected %s, got %s", i, typ, item.Events[i].Type)
		}
	}
}

// TestItemExpiry simulates an inbox item that isn't responded to before
// its deadline. A background job expires it.
func TestItemExpiry(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	deadline := time.Now().UTC().Add(-1 * time.Hour) // Already past.
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Verify your email address",
		Description: "Please verify your email to continue account opening.",
		Deadline:    &deadline,
		Actor:       "workflow:onboarding-999",
		Tags: []string{
			"type:action",
			"assignee:customer:CUST-0001",
			"priority:normal",
		},
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	if !inbox.HasTag(item, "deadline:"+deadline.Format(time.RFC3339)) {
		t.Error("expected deadline tag")
	}

	// Background job finds stale items and expires them.
	// Use age=0 since the item was just created — in production
	// this would be e.g. 24h, and the item would have been sitting
	// untouched for that long.
	stale, err := ib.Stale(ctx, []string{"status:open"}, 0, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("stale: %v", err)
	}

	found := false
	for _, s := range stale {
		if s.ID == item.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected item in stale results")
	}

	// Expire the item.
	item, err = ib.Expire(ctx, item.ID)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}

	if item.Status != inbox.StatusExpired {
		t.Errorf("expected expired, got %s", item.Status)
	}
	if !inbox.HasTag(item, "status:expired") {
		t.Error("expected status:expired tag")
	}

	// Cannot respond to expired item.
	_, err = ib.Respond(ctx, item.ID, inbox.Response{Actor: "user:x", Action: "submit"})
	if err == nil {
		t.Error("expected error responding to expired item")
	}
}

// TestCancelDuplicateItem simulates cancelling an item that was
// created in error or is a duplicate.
func TestCancelDuplicateItem(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Upload Emirates ID (duplicate)",
		Description: "This was created in error.",
		Actor:       "workflow:onboarding-456",
		Tags:        []string{"type:input_required", "assignee:customer:CUST-1234"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	item, err = ib.Cancel(ctx, item.ID, "user:ops:marco", "Duplicate item — original already completed")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if item.Status != inbox.StatusCancelled {
		t.Errorf("expected cancelled, got %s", item.Status)
	}

	// Verify cancellation event has reason.
	lastEvt := item.Events[len(item.Events)-1]
	if lastEvt.Type != "inbox.v1.ItemCancelled" {
		t.Errorf("expected ItemCancelled, got %s", lastEvt.Type)
	}

	var cancelData inboxv1.ItemCancelled
	if err := json.Unmarshal(lastEvt.Data, &cancelData); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cancelData.Reason != "Duplicate item — original already completed" {
		t.Errorf("unexpected reason: %s", cancelData.Reason)
	}

	// Cannot claim cancelled item.
	_, err = ib.Claim(ctx, item.ID, "user:anyone")
	if err == nil {
		t.Error("expected error claiming cancelled item")
	}
}

// TestMultipleItemsFromEligibilityEvaluation simulates a prodcat
// eligibility evaluation that produces multiple inbox items for
// different pending requirements — some for the customer, some
// for compliance.
func TestMultipleItemsFromEligibilityEvaluation(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	// Simulate: evaluation returned 3 pending requirements.
	requirements := []struct {
		title       string
		failureMode string
		team        string
		assignee    string
		requirement string
	}{
		{
			title:       "Verify your email address",
			failureMode: "actionable",
			assignee:    "assignee:customer:CUST-2000",
			requirement: "email_verified",
		},
		{
			title:       "Upload identity document",
			failureMode: "input_required",
			assignee:    "assignee:customer:CUST-2000",
			requirement: "valid_passport",
		},
		{
			title:       "Sanctions screening review",
			failureMode: "manual_review",
			team:        "team:compliance",
			assignee:    "assignee:team:compliance",
			requirement: "sanctions_screening_clear",
		},
	}

	var itemIDs []string
	for _, req := range requirements {
		tags := []string{
			"type:" + req.failureMode,
			"workflow:onboarding-multi",
			"ref:subscription:SUB-MULTI",
			"priority:normal",
		}
		if req.team != "" {
			tags = append(tags, req.team)
		}
		tags = append(tags, req.assignee)

		item, err := ib.Create(ctx, inbox.Meta{
			Title:       req.title,
			Description: "Required for Current Account opening.",
			PayloadType: "eligibility.v1.EligibilityReviewPayload",
			Payload: mustJSON(t, eligibilityReviewPayload{
				SubscriptionID:  "SUB-MULTI",
				ProductID:       "casa-aed",
				ProductName:     "Current Account — AED",
				RequirementName: req.requirement,
				FailureMode:     req.failureMode,
				CustomerID:      "CUST-2000",
			}),
			Actor: "workflow:onboarding-multi",
			Tags:  tags,
		})
		if err != nil {
			t.Fatalf("create %s: %v", req.requirement, err)
		}
		itemIDs = append(itemIDs, item.ID)
	}

	// Customer sees their 2 items.
	customerItems, err := ib.ListByTags(ctx, []string{"assignee:customer:CUST-2000", "status:open"}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list customer items: %v", err)
	}
	if len(customerItems) < 2 {
		t.Errorf("expected at least 2 customer items, got %d", len(customerItems))
	}

	// Compliance sees their 1 item.
	complianceItems, err := ib.ListByTags(ctx, []string{"team:compliance", "status:open"}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list compliance items: %v", err)
	}
	complianceCount := 0
	for _, it := range complianceItems {
		if inbox.HasTag(it, "workflow:onboarding-multi") {
			complianceCount++
		}
	}
	if complianceCount < 1 {
		t.Errorf("expected at least 1 compliance item, got %d", complianceCount)
	}

	// All items share the same workflow tag for correlation.
	workflowItems, err := ib.ListByTags(ctx, []string{"workflow:onboarding-multi"}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list workflow items: %v", err)
	}
	if len(workflowItems) < 3 {
		t.Errorf("expected at least 3 workflow items, got %d", len(workflowItems))
	}
}

// ─── Op builder tests ───

// TestOpBuilderRespondAndComplete demonstrates using the Op builder
// to respond, emit a custom domain event, update the payload, and
// complete the item — all in a single write.
func TestOpBuilderRespondAndComplete(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	// Create a compliance review item.
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Sanctions screening — Customer Z",
		Description: "Automated screening flagged a potential match.",
		PayloadType: "eligibility.v1.EligibilityReviewPayload",
		Payload: mustJSON(t, eligibilityReviewPayload{
			SubscriptionID:  "SUB-OP-001",
			ProductID:       "casa-aed",
			RequirementName: "sanctions_screening_clear",
			FailureMode:     "manual_review",
			CustomerID:      "CUST-OP-001",
		}),
		Actor: "workflow:onboarding-op",
		Tags: []string{
			"type:review",
			"team:compliance",
			"priority:high",
			"workflow:onboarding-op",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Compliance officer does everything in one operation:
	// - Responds with approve
	// - Updates the payload with the resolution
	// - Adds a comment
	// - Transitions to completed

	item, err = ib.On(ctx, item.ID, "user:compliance:fatima").
		Respond("approve", "False positive — name similarity only, different DOB and nationality.").
		UpdatePayload("eligibility.v1.ResolvedReviewPayload", mustJSON(t, map[string]any{
			"@type":                "type.googleapis.com/eligibility.v1.ResolvedReviewPayload",
			"original_requirement": "sanctions_screening_clear",
			"resolution":           "cleared",
			"resolved_by":          "user:compliance:fatima",
		})).
		Comment("Checked DOB and nationality against OFAC list. No match.").
		Tag("resolved:cleared").
		TransitionTo(inbox.StatusCompleted).
		Apply()

	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Verify final state.
	if item.Status != inbox.StatusCompleted {
		t.Errorf("expected completed, got %s", item.Status)
	}
	if item.PayloadType != "eligibility.v1.ResolvedReviewPayload" {
		t.Errorf("expected updated payload type, got %s", item.PayloadType)
	}
	if !inbox.HasTag(item, "resolved:cleared") {
		t.Error("expected resolved:cleared tag")
	}
	if !inbox.HasTag(item, "status:completed") {
		t.Error("expected status:completed tag")
	}

	// Verify all events were written in one batch.
	// created + responded + payload_updated + commented + completed = 5
	// (Tag() is a silent mutation — no event emitted)
	if len(item.Events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(item.Events))
	}

	// Verify event order matches the builder call order.
	expectedTypes2 := []string{
		"inbox.v1.ItemCreated",
		"inbox.v1.ItemResponded",
		"inbox.v1.PayloadUpdated",
		"inbox.v1.CommentAppended",
		"inbox.v1.ItemCompleted",
	}
	for i, typ := range expectedTypes2 {
		if item.Events[i].Type != typ {
			t.Errorf("event %d: expected %s, got %s", i, typ, item.Events[i].Type)
		}
	}
}

// TestOpBuilderWithProtoEvents demonstrates using WithEvent to emit
// custom domain events as proto messages, with type URLs derived
// automatically from the proto registry.
func TestOpBuilderWithProtoEvents(t *testing.T) {
	ib := sharedInbox(t)
	ctx := context.Background()

	item, err := ib.Create(ctx, inbox.Meta{
		Title: "KYC verification — Customer Y",
		Actor: "workflow:kyc-001",
		Tags:  []string{"type:review", "team:ops"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Use google.protobuf.Struct as a stand-in for domain proto messages.
	// In production these would be your own proto definitions like
	// kyc.v1.IDVCompleted, compliance.v1.ScreeningResolved, etc.
	idvResult, err := structpb.NewStruct(map[string]any{
		"liveness_passed":     true,
		"facial_match_passed": true,
		"confidence":          0.98,
	})
	if err != nil {
		t.Fatalf("new struct: %v", err)
	}

	addressResult, err := structpb.NewStruct(map[string]any{
		"validated": true,
		"method":    "utility_bill",
	})
	if err != nil {
		t.Fatalf("new struct: %v", err)
	}

	// Agent emits multiple check results as proto events.
	item, err = ib.On(ctx, item.ID, "agent:kyc-bot").
		WithEvent(idvResult).
		WithEvent(addressResult).
		Comment("All automated checks passed. Ready for final review.").
		Apply()

	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	// created + idv_completed + address_verified + commented = 4
	if len(item.Events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(item.Events))
	}

	// WithEvent derives the type from proto.MessageName.
	// google.protobuf.Struct is used as a stand-in for domain protos.
	expectedType := "google.protobuf.Struct"
	if item.Events[1].Type != expectedType {
		t.Errorf("expected %s, got %s", expectedType, item.Events[1].Type)
	}
	if item.Events[2].Type != expectedType {
		t.Errorf("expected %s, got %s", expectedType, item.Events[2].Type)
	}
	if item.Events[1].Actor != "agent:kyc-bot" {
		t.Errorf("expected agent:kyc-bot, got %s", item.Events[1].Actor)
	}
}
