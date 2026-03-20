package inbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/laenen-partners/entitystore"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	schemav1 "github.com/laenen-partners/inbox/schema/gen/schema/v1"
	"github.com/laenen-partners/tags"
)

// ─── Test infrastructure ───

var _sharedConnStr string

func sharedEntityStore(t *testing.T) *entitystore.EntityStore {
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

	return es
}

func sharedInbox(t *testing.T) *inbox.Inbox {
	t.Helper()
	return inbox.New(sharedEntityStore(t))
}

func ctxWithActor(principalID string, pt identity.PrincipalType) context.Context {
	id, _ := identity.New("test", "test", principalID, pt, nil)
	return identity.WithContext(context.Background(), id)
}

// ─── E2E Tests: KYC & Onboarding Scenarios ───

// TestCustomerMissingDocuments simulates a customer who starts onboarding
// but hasn't uploaded their identity document yet. The eligibility engine
// creates an inbox item asking the customer to upload. The customer
// eventually provides the document and responds.
func TestCustomerMissingDocuments(t *testing.T) {
	ib := sharedInbox(t)

	// 1. Workflow evaluates eligibility -> ID document is missing.
	//    Creates an inbox item for the customer.
	//    Use a structpb.Struct as payload since we don't have the real proto.
	payload, err := structpb.NewStruct(map[string]any{
		"subscription_id": "SUB-2026-0042",
		"product_name":    "Current Account -- AED",
		"accepted_types":  []any{"passport", "emirates_id"},
		"customer_id":     "CUST-1234",
	})
	if err != nil {
		t.Fatalf("new struct: %v", err)
	}

	ctx := ctxWithActor("onboarding-456", identity.PrincipalService)
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Upload your identity document",
		Description: "To continue opening your Current Account, please upload a valid passport or Emirates ID.",
		Payload:     payload,
		Tags: tags.MustNew(
			"type:input_required",
			"assignee:customer:cust-1234",
			"workflow:onboarding-456",
			"ref:subscription:sub-2026-0042",
			"priority:normal",
		),
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	if item.Proto.Status != inbox.StatusOpen {
		t.Errorf("expected status open, got %s", item.Proto.Status)
	}
	if item.Proto.PayloadType != "google.protobuf.Struct" {
		t.Errorf("expected payload type google.protobuf.Struct, got %s", item.Proto.PayloadType)
	}
	if !inbox.HasTag(item, "type:input_required") {
		t.Error("expected type:input_required tag")
	}

	// 2. Customer queries their inbox -- finds the open item.
	custCtx := ctxWithActor("customer:CUST-1234", identity.PrincipalUser)
	items, err := ib.ListByTags(custCtx, []string{"assignee:customer:cust-1234", tags.Status(inbox.StatusOpen)}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one item for customer")
	}

	// 3. Customer uploads document and responds.
	item, err = ib.Respond(custCtx, item.ID, inbox.Response{
		Action: "submit",
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	// Status is still open -- workflow decides when to complete.
	if item.Proto.Status != inbox.StatusOpen {
		t.Errorf("expected status open after respond, got %s", item.Proto.Status)
	}

	// 4. Verify event log records everything.
	events := item.Proto.Events
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].DataType != "inbox.v1.ItemCreated" {
		t.Errorf("expected ItemCreated event, got %s", events[0].DataType)
	}
	if events[1].DataType != "inbox.v1.ItemResponded" {
		t.Errorf("expected ItemResponded event, got %s", events[1].DataType)
	}
	if events[1].Actor != "user:customer:CUST-1234" {
		t.Errorf("expected customer actor, got %s", events[1].Actor)
	}

	// 5. Workflow re-evaluates and completes the item.
	wfCtx := ctxWithActor("onboarding-456", identity.PrincipalService)
	item, err = ib.Complete(wfCtx, item.ID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if item.Proto.Status != inbox.StatusCompleted {
		t.Errorf("expected completed, got %s", item.Proto.Status)
	}
	if !inbox.HasTag(item, tags.Status(inbox.StatusCompleted)) {
		t.Error("expected status:completed tag")
	}
}

// TestComplianceReviewPEPScreening simulates a PEP (Politically Exposed
// Person) screening that flags a customer during onboarding. A compliance
// officer must review and make a decision.
func TestComplianceReviewPEPScreening(t *testing.T) {
	ib := sharedInbox(t)

	// 1. Eligibility engine flags PEP screening -> creates compliance review item.
	payload, err := structpb.NewStruct(map[string]any{
		"subscription_id":  "SUB-2026-0042",
		"product_id":       "casa-aed",
		"product_name":     "Current Account -- AED",
		"requirement_name": "pep_screening_clear",
		"category":         "kyc",
		"failure_mode":     "manual_review",
		"resolution_hint":  "Review PEP screening report. Approve if risk is acceptable, reject if not.",
		"customer_id":      "CUST-1234",
		"party_id":         "PARTY-5678",
	})
	if err != nil {
		t.Fatalf("new struct: %v", err)
	}

	wfCtx := ctxWithActor("onboarding-456", identity.PrincipalService)
	item, err := ib.Create(wfCtx, inbox.Meta{
		Title:       "PEP screening review -- Ahmed K.",
		Description: "Customer flagged as Politically Exposed Person during onboarding for Current Account -- AED. Review screening report and decide.",
		Payload:     payload,
		Tags: tags.MustNew(
			"type:review",
			tags.Team("compliance"),
			"priority:high",
			"assignee:team:compliance",
			"ref:subscription:sub-2026-0042",
			"ref:customer:cust-1234",
			"workflow:onboarding-456",
		),
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	// 2. Compliance officer finds items needing review.
	fatimaCtx := ctxWithActor("compliance:fatima", identity.PrincipalUser)
	items, err := ib.ListByTags(fatimaCtx, []string{tags.Team("compliance"), tags.Status(inbox.StatusOpen)}, inbox.ListOpts{})
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
	item, err = ib.Claim(fatimaCtx, item.ID)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if item.Proto.Status != inbox.StatusClaimed {
		t.Errorf("expected claimed, got %s", item.Proto.Status)
	}

	// 4. Compliance officer adds an internal note.
	item, err = ib.Comment(fatimaCtx, item.ID,
		"Checked screening report. PEP status is historical -- former deputy minister, left office 2019. Low risk.",
		&inbox.CommentOpts{Visibility: []string{tags.Team("compliance")}},
	)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}

	// 5. Compliance officer approves.
	item, err = ib.Respond(fatimaCtx, item.ID, inbox.Response{
		Action: "approve",
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	// 6. Workflow completes.
	item, err = ib.Complete(wfCtx, item.ID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	// 7. Verify full event trail.
	events := item.Proto.Events
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	expectedTypes := []string{"inbox.v1.ItemCreated", "inbox.v1.ItemClaimed", "inbox.v1.CommentAppended", "inbox.v1.ItemResponded", "inbox.v1.ItemCompleted"}
	for i, typ := range expectedTypes {
		if events[i].DataType != typ {
			t.Errorf("event %d: expected %s, got %s", i, typ, events[i].DataType)
		}
	}
}

// TestRMOverrideOnBehalfOfClient simulates a relationship manager
// completing an action on behalf of a client who called the bank.
// The RM responds to the client's inbox item, and the event trail
// captures the delegation.
func TestRMOverrideOnBehalfOfClient(t *testing.T) {
	ib := sharedInbox(t)

	// 1. Workflow creates item for customer to accept terms.
	payload, err := structpb.NewStruct(map[string]any{
		"agreement_type":    "general_terms_and_conditions",
		"agreement_version": "2026.1",
		"subscription_id":   "SUB-2026-0099",
	})
	if err != nil {
		t.Fatalf("new struct: %v", err)
	}

	wfCtx := ctxWithActor("onboarding-789", identity.PrincipalService)
	item, err := ib.Create(wfCtx, inbox.Meta{
		Title:       "Accept Terms & Conditions",
		Description: "Please review and accept the General Terms & Conditions to continue your account opening.",
		Payload:     payload,
		Tags: tags.MustNew(
			"type:action",
			"assignee:customer:cust-5678",
			"rm:user:sarah",
			"workflow:onboarding-789",
			"priority:normal",
		),
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	// 2. Customer doesn't respond for a while. RM sees it in their view.
	sarahCtx := ctxWithActor("rm:sarah", identity.PrincipalUser)
	items, err := ib.ListByTags(sarahCtx, []string{"rm:user:sarah", tags.Status(inbox.StatusOpen)}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected item in RM's view")
	}

	// 3. Customer calls the bank. RM claims and acts on their behalf.
	item, err = ib.Claim(sarahCtx, item.ID)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	item, err = ib.Comment(sarahCtx, item.ID,
		"Client called branch. Read T&C over the phone, client verbally accepted.",
		nil,
	)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}

	// 4. RM responds on behalf of the client.
	item, err = ib.Respond(sarahCtx, item.ID, inbox.Response{
		Action: "accept",
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	// 5. Workflow completes.
	item, err = ib.Complete(wfCtx, item.ID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	// 6. Verify the RM is captured as the actor in the respond event.
	var respondEvt *inboxv1.Event
	for _, evt := range item.Proto.Events {
		if evt.DataType == "inbox.v1.ItemResponded" {
			respondEvt = evt
		}
	}
	if respondEvt == nil {
		t.Fatal("expected ItemResponded event")
	}
	if respondEvt.Actor != "user:rm:sarah" {
		t.Errorf("expected RM as actor, got %q", respondEvt.Actor)
	}

	// Verify the response action via the typed event data.
	var responded inboxv1.ItemResponded
	if err := respondEvt.Data.UnmarshalTo(&responded); err != nil {
		t.Fatalf("unmarshal responded data: %v", err)
	}
	if responded.Action != "accept" {
		t.Errorf("expected action accept, got %s", responded.Action)
	}
}

// TestEscalationFromOpsToCompliance simulates an ops team member
// escalating an item to compliance when they discover it needs
// specialist review.
func TestEscalationFromOpsToCompliance(t *testing.T) {
	ib := sharedInbox(t)

	// 1. Item created for ops team.
	payload, err := structpb.NewStruct(map[string]any{
		"subscription_id":  "SUB-2026-0150",
		"product_id":       "casa-aed",
		"product_name":     "Current Account -- AED",
		"requirement_name": "source_of_funds_verified",
		"category":         "kyc",
		"failure_mode":     "manual_review",
		"resolution_hint":  "Review source of funds documentation.",
		"customer_id":      "CUST-9999",
		"party_id":         "PARTY-9999",
	})
	if err != nil {
		t.Fatalf("new struct: %v", err)
	}

	wfCtx := ctxWithActor("monitoring-100", identity.PrincipalService)
	item, err := ib.Create(wfCtx, inbox.Meta{
		Title:       "Source of funds verification -- Large deposit",
		Description: "Customer deposited $250,000. Source of funds documentation required per AML policy.",
		Payload:     payload,
		Tags: tags.MustNew(
			"type:review",
			tags.Team("ops"),
			"priority:high",
			"ref:customer:cust-9999",
		),
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	// 2. Ops team member claims and reviews.
	marcoCtx := ctxWithActor("ops:marco", identity.PrincipalUser)
	item, err = ib.Claim(marcoCtx, item.ID)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	item, err = ib.Comment(marcoCtx, item.ID,
		"Amount exceeds $100k threshold. Customer is a PEP. Escalating to compliance for enhanced due diligence.",
		nil,
	)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}

	// 3. Escalate to compliance.
	item, err = ib.Escalate(marcoCtx, item.ID, "ops", "compliance",
		"Amount exceeds threshold and customer is flagged PEP. Needs enhanced due diligence.")
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}

	// 4. Verify team tag was updated.
	item, err = ib.Get(marcoCtx, item.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if inbox.HasTag(item, tags.Team("ops")) {
		t.Error("expected team:ops tag to be removed")
	}
	if !inbox.HasTag(item, tags.Team("compliance")) {
		t.Error("expected team:compliance tag")
	}

	// 5. Verify escalation event.
	var escalationEvt *inboxv1.Event
	for _, evt := range item.Proto.Events {
		if evt.DataType == "inbox.v1.ItemEscalated" {
			escalationEvt = evt
		}
	}
	if escalationEvt == nil {
		t.Fatal("expected ItemEscalated event")
	}

	var escData inboxv1.ItemEscalated
	if err := escalationEvt.Data.UnmarshalTo(&escData); err != nil {
		t.Fatalf("unmarshal escalation: %v", err)
	}
	if escData.FromTeam != "ops" || escData.ToTeam != "compliance" {
		t.Errorf("expected ops->compliance, got %s->%s", escData.FromTeam, escData.ToTeam)
	}

	// 6. Compliance picks up and resolves.
	// Release first (ops had it claimed), then compliance claims.
	item, err = ib.Release(marcoCtx, item.ID)
	if err != nil {
		t.Fatalf("release: %v", err)
	}

	fatimaCtx := ctxWithActor("compliance:fatima", identity.PrincipalUser)
	item, err = ib.Claim(fatimaCtx, item.ID)
	if err != nil {
		t.Fatalf("claim by compliance: %v", err)
	}

	item, err = ib.Respond(fatimaCtx, item.ID, inbox.Response{
		Action:  "approve",
		Comment: "EDD completed. Source of funds verified via bank statements and employment letter.",
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	item, err = ib.Complete(wfCtx, item.ID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	// 7. Full event trail.
	events := item.Proto.Events
	if len(events) != 8 {
		t.Fatalf("expected 8 events, got %d", len(events))
	}
	expectedTypes := []string{
		"inbox.v1.ItemCreated", "inbox.v1.ItemClaimed", "inbox.v1.CommentAppended", "inbox.v1.ItemEscalated",
		"inbox.v1.ItemReleased", "inbox.v1.ItemClaimed", "inbox.v1.ItemResponded", "inbox.v1.ItemCompleted",
	}
	for i, typ := range expectedTypes {
		if events[i].DataType != typ {
			t.Errorf("event %d: expected %s, got %s", i, typ, events[i].DataType)
		}
	}
}

// TestItemExpiry simulates an inbox item that isn't responded to before
// its deadline. A background job expires it.
func TestItemExpiry(t *testing.T) {
	ib := sharedInbox(t)

	wfCtx := ctxWithActor("onboarding-999", identity.PrincipalService)
	deadline := time.Now().UTC().Add(-1 * time.Hour) // Already past.
	item, err := ib.Create(wfCtx, inbox.Meta{
		Title:       "Verify your email address",
		Description: "Please verify your email to continue account opening.",
		Deadline:    &deadline,
		Tags: tags.MustNew(
			"type:action",
			"assignee:customer:cust-0001",
			"priority:normal",
		),
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	if inbox.TagValue(item, "deadline") == "" {
		t.Error("expected deadline tag")
	}

	// Background job finds stale items and expires them.
	sysCtx := ctxWithActor("expiry-job", identity.PrincipalService)
	stale, err := ib.Stale(sysCtx, []string{tags.Status(inbox.StatusOpen)}, 0, inbox.ListOpts{})
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
	item, err = ib.Expire(sysCtx, item.ID)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}

	if item.Proto.Status != inbox.StatusExpired {
		t.Errorf("expected expired, got %s", item.Proto.Status)
	}
	if !inbox.HasTag(item, tags.Status(inbox.StatusExpired)) {
		t.Error("expected status:expired tag")
	}

	// Cannot respond to expired item.
	userCtx := ctxWithActor("x", identity.PrincipalUser)
	_, err = ib.Respond(userCtx, item.ID, inbox.Response{Action: "submit"})
	if err == nil {
		t.Error("expected error responding to expired item")
	}
}

// TestCancelDuplicateItem simulates cancelling an item that was
// created in error or is a duplicate.
func TestCancelDuplicateItem(t *testing.T) {
	ib := sharedInbox(t)

	wfCtx := ctxWithActor("onboarding-456", identity.PrincipalService)
	item, err := ib.Create(wfCtx, inbox.Meta{
		Title:       "Upload Emirates ID (duplicate)",
		Description: "This was created in error.",
		Tags:        tags.MustNew("type:input_required", "assignee:customer:cust-1234"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	marcoCtx := ctxWithActor("ops:marco", identity.PrincipalUser)
	item, err = ib.Cancel(marcoCtx, item.ID, "Duplicate item -- original already completed")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if item.Proto.Status != inbox.StatusCancelled {
		t.Errorf("expected cancelled, got %s", item.Proto.Status)
	}

	// Verify cancellation event has reason.
	events := item.Proto.Events
	lastEvt := events[len(events)-1]
	if lastEvt.DataType != "inbox.v1.ItemCancelled" {
		t.Errorf("expected ItemCancelled, got %s", lastEvt.DataType)
	}

	var cancelData inboxv1.ItemCancelled
	if err := lastEvt.Data.UnmarshalTo(&cancelData); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cancelData.Reason != "Duplicate item -- original already completed" {
		t.Errorf("unexpected reason: %s", cancelData.Reason)
	}

	// Cannot claim cancelled item.
	anyCtx := ctxWithActor("anyone", identity.PrincipalUser)
	_, err = ib.Claim(anyCtx, item.ID)
	if err == nil {
		t.Error("expected error claiming cancelled item")
	}
}

// TestMultipleItemsFromEligibilityEvaluation simulates a prodcat
// eligibility evaluation that produces multiple inbox items for
// different pending requirements -- some for the customer, some
// for compliance.
func TestMultipleItemsFromEligibilityEvaluation(t *testing.T) {
	ib := sharedInbox(t)
	ctx := ctxWithActor("onboarding-multi", identity.PrincipalService)

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
			assignee:    "assignee:customer:cust-2000",
			requirement: "email_verified",
		},
		{
			title:       "Upload identity document",
			failureMode: "input_required",
			assignee:    "assignee:customer:cust-2000",
			requirement: "valid_passport",
		},
		{
			title:       "Sanctions screening review",
			failureMode: "manual_review",
			team:        tags.Team("compliance"),
			assignee:    "assignee:team:compliance",
			requirement: "sanctions_screening_clear",
		},
	}

	var itemIDs []string
	for _, req := range requirements {
		itemTags := tags.MustNew(
			"type:"+req.failureMode,
			"workflow:onboarding-multi",
			"ref:subscription:sub-multi",
			"priority:normal",
		)
		if req.team != "" {
			itemTags = itemTags.Merge(tags.MustNew(req.team))
		}
		itemTags = itemTags.Merge(tags.MustNew(req.assignee))

		payload, err := structpb.NewStruct(map[string]any{
			"subscription_id":  "SUB-MULTI",
			"product_id":       "casa-aed",
			"product_name":     "Current Account -- AED",
			"requirement_name": req.requirement,
			"failure_mode":     req.failureMode,
			"customer_id":      "CUST-2000",
		})
		if err != nil {
			t.Fatalf("new struct: %v", err)
		}

		item, err := ib.Create(ctx, inbox.Meta{
			Title:       req.title,
			Description: "Required for Current Account opening.",
			Payload:     payload,
			Tags:        itemTags,
		})
		if err != nil {
			t.Fatalf("create %s: %v", req.requirement, err)
		}
		_ = append(itemIDs, item.ID)
	}

	// Customer sees their 2 items.
	customerItems, err := ib.ListByTags(ctx, []string{"assignee:customer:cust-2000", tags.Status(inbox.StatusOpen)}, inbox.ListOpts{})
	if err != nil {
		t.Fatalf("list customer items: %v", err)
	}
	if len(customerItems) < 2 {
		t.Errorf("expected at least 2 customer items, got %d", len(customerItems))
	}

	// Compliance sees their 1 item.
	complianceItems, err := ib.ListByTags(ctx, []string{tags.Team("compliance"), tags.Status(inbox.StatusOpen)}, inbox.ListOpts{})
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
// complete the item -- all in a single write.
func TestOpBuilderRespondAndComplete(t *testing.T) {
	ib := sharedInbox(t)

	// Create a compliance review item.
	payload, err := structpb.NewStruct(map[string]any{
		"subscription_id":  "SUB-OP-001",
		"product_id":       "casa-aed",
		"requirement_name": "sanctions_screening_clear",
		"failure_mode":     "manual_review",
		"customer_id":      "CUST-OP-001",
	})
	if err != nil {
		t.Fatalf("new struct: %v", err)
	}

	wfCtx := ctxWithActor("onboarding-op", identity.PrincipalService)
	item, err := ib.Create(wfCtx, inbox.Meta{
		Title:       "Sanctions screening -- Customer Z",
		Description: "Automated screening flagged a potential match.",
		Payload:     payload,
		Tags: tags.MustNew(
			"type:review",
			tags.Team("compliance"),
			"priority:high",
			"workflow:onboarding-op",
		),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Compliance officer does everything in one operation:
	// - Responds with approve
	// - Updates the payload with the resolution
	// - Adds a comment
	// - Transitions to completed

	resolvedPayload, err := structpb.NewStruct(map[string]any{
		"original_requirement": "sanctions_screening_clear",
		"resolution":           "cleared",
		"resolved_by":          "user:compliance:fatima",
	})
	if err != nil {
		t.Fatalf("new struct: %v", err)
	}

	// The Op.UpdatePayload still takes json.RawMessage for backwards compat,
	// so we marshal the struct payload.
	resolvedJSON, err := resolvedPayload.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	fatimaCtx := ctxWithActor("compliance:fatima", identity.PrincipalUser)
	item, err = ib.On(fatimaCtx, item.ID).
		Respond("approve", "False positive -- name similarity only, different DOB and nationality.").
		UpdatePayload("google.protobuf.Struct", resolvedJSON).
		Comment("Checked DOB and nationality against OFAC list. No match.").
		Tag("resolved:cleared").
		TransitionTo(inbox.StatusCompleted).
		Apply()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Verify final state.
	if item.Proto.Status != inbox.StatusCompleted {
		t.Errorf("expected completed, got %s", item.Proto.Status)
	}
	if item.Proto.PayloadType != "google.protobuf.Struct" {
		t.Errorf("expected updated payload type, got %s", item.Proto.PayloadType)
	}
	if !inbox.HasTag(item, "resolved:cleared") {
		t.Error("expected resolved:cleared tag")
	}
	if !inbox.HasTag(item, tags.Status(inbox.StatusCompleted)) {
		t.Error("expected status:completed tag")
	}

	// Verify all events were written in one batch.
	// created + responded + payload_updated + commented + completed = 5
	// (Tag() is a silent mutation -- no event emitted)
	events := item.Proto.Events
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
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
		if events[i].DataType != typ {
			t.Errorf("event %d: expected %s, got %s", i, typ, events[i].DataType)
		}
	}
}

// TestOpBuilderWithProtoEvents demonstrates using WithEvent to emit
// custom domain events as proto messages, with type URLs derived
// automatically from the proto registry.
func TestOpBuilderWithProtoEvents(t *testing.T) {
	ib := sharedInbox(t)

	wfCtx := ctxWithActor("kyc-001", identity.PrincipalService)
	item, err := ib.Create(wfCtx, inbox.Meta{
		Title: "KYC verification -- Customer Y",
		Tags:  tags.MustNew("type:review", tags.Team("ops")),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Use google.protobuf.Struct as a stand-in for domain proto messages.
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
	botCtx := ctxWithActor("kyc-bot", identity.PrincipalService)
	item, err = ib.On(botCtx, item.ID).
		WithEvent(idvResult).
		WithEvent(addressResult).
		Comment("All automated checks passed. Ready for final review.").
		Apply()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	// created + idv_completed + address_verified + commented = 4
	events := item.Proto.Events
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	// WithEvent derives the type from proto.MessageName.
	expectedType := "google.protobuf.Struct"
	if events[1].DataType != expectedType {
		t.Errorf("expected %s, got %s", expectedType, events[1].DataType)
	}
	if events[2].DataType != expectedType {
		t.Errorf("expected %s, got %s", expectedType, events[2].DataType)
	}
	if events[1].Actor != "service:kyc-bot" {
		t.Errorf("expected service:kyc-bot, got %s", events[1].Actor)
	}
}

// ─── Lifecycle hooks tests ───

func TestLifecycleHooks(t *testing.T) {
	es := sharedEntityStore(t)
	recorder := &hookRecorder{}
	ib := inbox.New(es,
		inbox.WithLifecycleHooks("schema.v1.ItemSchema", recorder),
	)

	actorCtx := ctxWithActor("hook-tester", identity.PrincipalUser)

	item, err := ib.Create(actorCtx, inbox.Meta{
		Title: "Hook Test Item",
		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{{Label: "Test", Value: "Value"}},
		},
	})
	require.NoError(t, err)

	// Claim → OnClaim
	_, err = ib.Claim(actorCtx, item.ID)
	require.NoError(t, err)
	require.Equal(t, 1, recorder.claimCount)
	require.Equal(t, item.ID, recorder.lastItemID)

	// Release → OnRelease
	_, err = ib.Release(actorCtx, item.ID)
	require.NoError(t, err)
	require.Equal(t, 1, recorder.releaseCount)

	// Complete → OnComplete
	_, err = ib.Complete(actorCtx, item.ID)
	require.NoError(t, err)
	require.Equal(t, 1, recorder.completeCount)

	// Cancel → OnCancel
	cancelItem, err := ib.Create(actorCtx, inbox.Meta{
		Title: "Hook Test Cancel Item",
		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{{Label: "Test", Value: "Value"}},
		},
	})
	require.NoError(t, err)
	_, err = ib.Cancel(actorCtx, cancelItem.ID, "test cancellation")
	require.NoError(t, err)
	require.Equal(t, 1, recorder.cancelCount)
	require.Equal(t, cancelItem.ID, recorder.lastItemID)

	// Expire → OnExpire
	expireItem, err := ib.Create(actorCtx, inbox.Meta{
		Title: "Hook Test Expire Item",
		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{{Label: "Test", Value: "Value"}},
		},
	})
	require.NoError(t, err)
	_, err = ib.Expire(actorCtx, expireItem.ID)
	require.NoError(t, err)
	require.Equal(t, 1, recorder.expireCount)
	require.Equal(t, expireItem.ID, recorder.lastItemID)

	// Op path → hooks fire through the Op builder too
	opItem, err := ib.Create(actorCtx, inbox.Meta{
		Title: "Hook Test Op Item",
		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{{Label: "Test", Value: "Value"}},
		},
	})
	require.NoError(t, err)
	prevCompleteCount := recorder.completeCount
	_, err = ib.On(actorCtx, opItem.ID).TransitionTo(inbox.StatusCompleted).Apply()
	require.NoError(t, err)
	require.Equal(t, prevCompleteCount+1, recorder.completeCount)
}

type hookRecorder struct {
	inbox.DefaultHooks
	claimCount, releaseCount, completeCount, cancelCount, expireCount int
	lastItemID                                                       string
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
