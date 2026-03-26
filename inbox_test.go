package inbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/laenen-partners/entitystore"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
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

func seedOpenItem(t *testing.T, ib *inbox.Inbox) inbox.Item {
	t.Helper()
	ctx := ctxWithActor("seed-service", identity.PrincipalService)
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Seed item",
		Description: "Created by test helper.",
		Tags:        tags.MustNew("type:test"),
	})
	require.NoError(t, err)
	return item
}

// ─── Tests ───

func TestCreateAndGet(t *testing.T) {
	ib := sharedInbox(t)

	payload, err := structpb.NewStruct(map[string]any{
		"customer_id": "CUST-001",
		"product":     "savings-aed",
	})
	require.NoError(t, err)

	ctx := ctxWithActor("onboarding-svc", identity.PrincipalService)
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Upload identity document",
		Description: "Please upload a valid passport or Emirates ID.",
		Payload:     payload,
		Tags: tags.MustNew(
			"type:input_required",
			"assignee:customer:cust-001",
			"workflow:onboarding-1",
		),
	})
	require.NoError(t, err)

	assert.Equal(t, "Upload identity document", item.Title())
	assert.Equal(t, "Please upload a valid passport or Emirates ID.", item.Description())
	assert.Equal(t, inbox.StatusOpen, item.Status())
	assert.Equal(t, "google.protobuf.Struct", item.PayloadType())
	assert.True(t, inbox.HasTag(item, "type:input_required"))
	statusOpenTag, _ := tags.Status(inbox.StatusOpen)
	assert.True(t, inbox.HasTag(item, statusOpenTag))

	// Verify ItemCreated event.
	require.Len(t, item.Events(), 1)
	assert.Equal(t, "inbox.v1.ItemCreated", item.Events()[0].DataType)

	// Re-fetch via Get and verify.
	fetched, err := ib.Get(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, item.ID, fetched.ID)
	assert.Equal(t, item.Title(), fetched.Title())
	assert.Equal(t, inbox.StatusOpen, fetched.Status())
}

func TestClaimAndRelease(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	userCtx := ctxWithActor("ops:marco", identity.PrincipalUser)

	assigneeTag, _ := tags.Build("assignee", "user:ops:marco")

	// Claim the item.
	claimed, err := ib.Claim(userCtx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, inbox.StatusClaimed, claimed.Status())
	assert.True(t, inbox.HasTag(claimed, assigneeTag))

	// Release the item.
	released, err := ib.Release(userCtx, claimed.ID)
	require.NoError(t, err)
	assert.Equal(t, inbox.StatusOpen, released.Status())
	// The in-memory item returned by Release has the assignee tag removed.
	assert.False(t, inbox.HasTag(released, assigneeTag))

	// Re-fetch to verify status is open and events are recorded.
	fetched, err := ib.Get(userCtx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, inbox.StatusOpen, fetched.Status())

	// Verify event trail: created + claimed + released.
	require.Len(t, fetched.Events(), 3)
	assert.Equal(t, "inbox.v1.ItemCreated", fetched.Events()[0].DataType)
	assert.Equal(t, "inbox.v1.ItemClaimed", fetched.Events()[1].DataType)
	assert.Equal(t, "inbox.v1.ItemReleased", fetched.Events()[2].DataType)
}

func TestCloseFromOpen(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	ctx := ctxWithActor("workflow-svc", identity.PrincipalService)
	closed, err := ib.Close(ctx, item.ID, "requirement satisfied")
	require.NoError(t, err)
	assert.Equal(t, inbox.StatusClosed, closed.Status())

	// Verify ItemClosed event.
	events := closed.Events()
	require.Len(t, events, 2) // created + closed
	assert.Equal(t, "inbox.v1.ItemClosed", events[1].DataType)

	var closedData inboxv1.ItemClosed
	require.NoError(t, events[1].Data.UnmarshalTo(&closedData))
	assert.Equal(t, "requirement satisfied", closedData.Reason)
}

func TestCloseFromClaimed(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	userCtx := ctxWithActor("ops:fatima", identity.PrincipalUser)
	claimed, err := ib.Claim(userCtx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, inbox.StatusClaimed, claimed.Status())

	svcCtx := ctxWithActor("workflow-svc", identity.PrincipalService)
	closed, err := ib.Close(svcCtx, claimed.ID, "done")
	require.NoError(t, err)
	assert.Equal(t, inbox.StatusClosed, closed.Status())

	// Verify event trail: created + claimed + closed.
	require.Len(t, closed.Events(), 3)
	assert.Equal(t, "inbox.v1.ItemClosed", closed.Events()[2].DataType)
}

func TestCloseTerminalFails(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	ctx := ctxWithActor("workflow-svc", identity.PrincipalService)
	_, err := ib.Close(ctx, item.ID, "first close")
	require.NoError(t, err)

	// Closing again should fail with ErrTerminalStatus.
	_, err = ib.Close(ctx, item.ID, "second close")
	require.Error(t, err)
	assert.True(t, errors.Is(err, inbox.ErrTerminalStatus))
}

func TestComment(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	teamCompliance, _ := tags.Team("compliance")
	ctx := ctxWithActor("compliance:fatima", identity.PrincipalUser)
	commented, err := ib.Comment(ctx, item.ID,
		"Checked screening report. PEP status is historical, low risk.",
		&inbox.CommentOpts{Visibility: []string{teamCompliance}},
	)
	require.NoError(t, err)

	events := commented.Events()
	require.Len(t, events, 2) // created + comment
	assert.Equal(t, "inbox.v1.CommentAppended", events[1].DataType)
	assert.Equal(t, "user:compliance:fatima", events[1].Actor)

	var commentData inboxv1.CommentAppended
	require.NoError(t, events[1].Data.UnmarshalTo(&commentData))
	assert.Equal(t, "Checked screening report. PEP status is historical, low risk.", commentData.Body)
	assert.Contains(t, commentData.Visibility, teamCompliance)
}

func TestReassign(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	ctx := ctxWithActor("ops:marco", identity.PrincipalUser)
	reassigned, err := ib.Reassign(ctx, item.ID, "team:ops", "team:compliance", "Needs specialist review")
	require.NoError(t, err)

	events := reassigned.Events()
	require.Len(t, events, 2) // created + reassigned
	assert.Equal(t, "inbox.v1.ItemReassigned", events[1].DataType)

	var reassignData inboxv1.ItemReassigned
	require.NoError(t, events[1].Data.UnmarshalTo(&reassignData))
	assert.Equal(t, "team:ops", reassignData.FromActor)
	assert.Equal(t, "team:compliance", reassignData.ToActor)
	assert.Equal(t, "Needs specialist review", reassignData.Reason)
}

func TestAddEvent(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	// Use a structpb.Struct as a stand-in for a custom domain event.
	customData, err := structpb.NewStruct(map[string]any{
		"check_type": "liveness",
		"passed":     true,
	})
	require.NoError(t, err)

	packed, err := anypb.New(customData)
	require.NoError(t, err)

	evt := &inboxv1.Event{
		Actor:    "service:kyc-bot",
		DataType: string(proto.MessageName(customData)),
		Data:     packed,
	}

	ctx := ctxWithActor("kyc-bot", identity.PrincipalService)
	updated, err := ib.AddEvent(ctx, item.ID, evt)
	require.NoError(t, err)

	events := updated.Events()
	require.Len(t, events, 2) // created + custom
	assert.Equal(t, "google.protobuf.Struct", events[1].DataType)
	assert.Equal(t, "service:kyc-bot", events[1].Actor)
}

func TestTagAndUntag(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	ctx := ctxWithActor("ops:marco", identity.PrincipalUser)

	// Add a tag.
	err := ib.Tag(ctx, item.ID, "priority:high")
	require.NoError(t, err)

	// Re-fetch to verify the tag and the TagsChanged event.
	fetched, err := ib.Get(ctx, item.ID)
	require.NoError(t, err)
	assert.True(t, inbox.HasTag(fetched, "priority:high"))

	// Find the TagsChanged event for the addition.
	var addEvt *inboxv1.Event
	for _, e := range fetched.Events() {
		if e.DataType == "inbox.v1.TagsChanged" {
			addEvt = e
			break
		}
	}
	require.NotNil(t, addEvt, "expected TagsChanged event after Tag")

	var addData inboxv1.TagsChanged
	require.NoError(t, addEvt.Data.UnmarshalTo(&addData))
	assert.Contains(t, addData.Added, "priority:high")

	// Remove the tag.
	err = ib.Untag(ctx, item.ID, "priority:high")
	require.NoError(t, err)

	fetched, err = ib.Get(ctx, item.ID)
	require.NoError(t, err)
	assert.False(t, inbox.HasTag(fetched, "priority:high"))

	// Find the TagsChanged event for the removal (last one).
	var removeEvt *inboxv1.Event
	for _, e := range fetched.Events() {
		if e.DataType == "inbox.v1.TagsChanged" {
			removeEvt = e // keep iterating to get the last one
		}
	}
	require.NotNil(t, removeEvt)

	var removeData inboxv1.TagsChanged
	require.NoError(t, removeEvt.Data.UnmarshalTo(&removeData))
	assert.Contains(t, removeData.Removed, "priority:high")
}

func TestListByTags(t *testing.T) {
	ib := sharedInbox(t)
	ctx := ctxWithActor("onboarding-multi", identity.PrincipalService)

	// Create items with different tags.
	_, err := ib.Create(ctx, inbox.Meta{
		Title: "Customer item A",
		Tags:  tags.MustNew("type:action", "assignee:customer:cust-list-1", "workflow:list-test"),
	})
	require.NoError(t, err)

	_, err = ib.Create(ctx, inbox.Meta{
		Title: "Customer item B",
		Tags:  tags.MustNew("type:action", "assignee:customer:cust-list-1", "workflow:list-test"),
	})
	require.NoError(t, err)

	teamCompliance, _ := tags.Team("compliance")
	statusOpen, _ := tags.Status(inbox.StatusOpen)
	_, err = ib.Create(ctx, inbox.Meta{
		Title: "Compliance item",
		Tags:  tags.MustNew("type:review", teamCompliance, "workflow:list-test"),
	})
	require.NoError(t, err)

	// List customer items.
	customerItems, err := ib.ListByTags(ctx, []string{"assignee:customer:cust-list-1", statusOpen}, inbox.ListOpts{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(customerItems), 2)

	// List compliance items.
	complianceItems, err := ib.ListByTags(ctx, []string{teamCompliance, "workflow:list-test"}, inbox.ListOpts{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(complianceItems), 1)

	// All items share the workflow tag.
	workflowItems, err := ib.ListByTags(ctx, []string{"workflow:list-test"}, inbox.ListOpts{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(workflowItems), 3)
}

func TestSearch(t *testing.T) {
	ib := sharedInbox(t)
	ctx := ctxWithActor("search-svc", identity.PrincipalService)

	_, err := ib.Create(ctx, inbox.Meta{
		Title:       "Biometric liveness verification required",
		Description: "Customer must complete biometric liveness check for account opening.",
		Tags:        tags.MustNew("type:action", "workflow:search-test"),
	})
	require.NoError(t, err)

	// Search for a keyword present in the title.
	results, err := ib.Search(ctx, "biometric liveness", inbox.ListOpts{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)

	found := false
	for _, r := range results {
		if r.Title() == "Biometric liveness verification required" {
			found = true
		}
	}
	assert.True(t, found, "expected to find item by title keyword search")
}

func TestMultipleItemsFromEligibilityEvaluation(t *testing.T) {
	ib := sharedInbox(t)
	ctx := ctxWithActor("onboarding-multi", identity.PrincipalService)
	teamCompliance, _ := tags.Team("compliance")
	statusOpen, _ := tags.Status(inbox.StatusOpen)

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
			team:        teamCompliance,
			assignee:    "assignee:team:compliance",
			requirement: "sanctions_screening_clear",
		},
	}

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
		require.NoError(t, err)

		_, err = ib.Create(ctx, inbox.Meta{
			Title:       req.title,
			Description: "Required for Current Account opening.",
			Payload:     payload,
			Tags:        itemTags,
		})
		require.NoError(t, err)
	}

	// Customer sees their 2 items.
	customerItems, err := ib.ListByTags(ctx, []string{"assignee:customer:cust-2000", statusOpen}, inbox.ListOpts{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(customerItems), 2)

	// Compliance sees their 1 item.
	complianceItems, err := ib.ListByTags(ctx, []string{teamCompliance, statusOpen}, inbox.ListOpts{})
	require.NoError(t, err)
	complianceCount := 0
	for _, it := range complianceItems {
		if inbox.HasTag(it, "workflow:onboarding-multi") {
			complianceCount++
		}
	}
	assert.GreaterOrEqual(t, complianceCount, 1)

	// All items share the same workflow tag for correlation.
	workflowItems, err := ib.ListByTags(ctx, []string{"workflow:onboarding-multi"}, inbox.ListOpts{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(workflowItems), 3)
}
