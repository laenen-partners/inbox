package inbox_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/tags"
)

// ─── Test helpers for dispatcher tests ───

// mockDispatcher implements inbox.Dispatcher for testing.
type mockDispatcher struct {
	called bool
	err    error
}

func (d *mockDispatcher) Dispatch(_ context.Context, _ string, _ string, _ inbox.Response) error {
	d.called = true
	return d.err
}

// seedOpenItemWithCallback creates an open inbox item with a callback tag.
func seedOpenItemWithCallback(t *testing.T, ib *inbox.Inbox, callback string) inbox.Item {
	t.Helper()
	ctx := ctxWithActor("test-workflow", identity.PrincipalService)
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Callback test item",
		Description: "Created for two-phase apply tests",
		Tags:        tags.MustNew("type:test", "callback:"+callback),
	})
	require.NoError(t, err)
	require.Equal(t, inbox.StatusOpen, item.Proto.Status)
	return item
}

// seedOpenItem creates a minimal open inbox item for testing.
func seedOpenItem(t *testing.T, ib *inbox.Inbox) inbox.Item {
	t.Helper()
	ctx := ctxWithActor("test-workflow", identity.PrincipalService)
	item, err := ib.Create(ctx, inbox.Meta{
		Title:       "Test item",
		Description: "Created for op_test",
		Tags:        tags.MustNew("type:test"),
	})
	require.NoError(t, err)
	require.Equal(t, inbox.StatusOpen, item.Proto.Status)
	return item
}

func TestRespondWithData(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	// Build response data — simulates form field values from an operator.
	data := json.RawMessage(`{"decision":"approved","risk_score":42}`)

	ctx := ctxWithActor("operator-1", identity.PrincipalUser)
	updated, err := ib.On(ctx, item.ID).
		RespondWithData("approve", "Looks good", data).
		Apply()
	require.NoError(t, err)

	// Verify the response event was recorded.
	events := updated.Proto.Events
	// Event 0: ItemCreated, Event 1: ItemResponded
	require.Len(t, events, 2)
	require.Equal(t, "inbox.v1.ItemResponded", events[1].DataType)
	require.Equal(t, "approve", events[1].Detail)

	// Unpack the ItemResponded proto and verify fields.
	responded := &inboxv1.ItemResponded{}
	err = events[1].Data.UnmarshalTo(responded)
	require.NoError(t, err)
	require.Equal(t, "approve", responded.Action)
	require.Equal(t, "Looks good", responded.Comment)

	// Verify the payload field contains our data.
	require.NotNil(t, responded.Payload, "ItemResponded.Payload should be set")
	st := &structpb.Struct{}
	err = responded.Payload.UnmarshalTo(st)
	require.NoError(t, err)

	// Round-trip: marshal the struct back to JSON and compare.
	roundTripped, err := json.Marshal(st.AsMap())
	require.NoError(t, err)

	var expected, actual map[string]any
	require.NoError(t, json.Unmarshal(data, &expected))
	require.NoError(t, json.Unmarshal(roundTripped, &actual))
	require.Equal(t, expected["decision"], actual["decision"])
	require.InDelta(t, expected["risk_score"], actual["risk_score"], 0.001)
}

func TestRespondWithData_NilData(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	// RespondWithData with nil data should behave like Respond.
	ctx := ctxWithActor("operator-2", identity.PrincipalUser)
	updated, err := ib.On(ctx, item.ID).
		RespondWithData("reject", "Missing docs", nil).
		Apply()
	require.NoError(t, err)

	events := updated.Proto.Events
	require.Len(t, events, 2)

	responded := &inboxv1.ItemResponded{}
	err = events[1].Data.UnmarshalTo(responded)
	require.NoError(t, err)
	require.Equal(t, "reject", responded.Action)
	require.Equal(t, "Missing docs", responded.Comment)
	require.Nil(t, responded.Payload, "Payload should be nil when no data provided")
}

func TestRespond_StillWorks(t *testing.T) {
	ib := sharedInbox(t)
	item := seedOpenItem(t, ib)

	// The original Respond method should continue to work unchanged.
	ctx := ctxWithActor("operator-3", identity.PrincipalUser)
	updated, err := ib.On(ctx, item.ID).
		Respond("approve", "LGTM").
		Apply()
	require.NoError(t, err)

	events := updated.Proto.Events
	require.Len(t, events, 2)

	responded := &inboxv1.ItemResponded{}
	err = events[1].Data.UnmarshalTo(responded)
	require.NoError(t, err)
	require.Equal(t, "approve", responded.Action)
	require.Equal(t, "LGTM", responded.Comment)
	require.Nil(t, responded.Payload, "Original Respond should not set Payload")
}

// ─── Two-phase Apply tests ───

func TestOp_TwoPhaseApply_DispatchError(t *testing.T) {
	es := sharedEntityStore(t)
	disp := &mockDispatcher{err: fmt.Errorf("connection refused")}
	ib := inbox.New(es, inbox.WithDispatcher(disp))

	item := seedOpenItemWithCallback(t, ib, "dbos:wf-123")

	// Respond + TransitionTo completed with a dispatcher that fails.
	ctx := ctxWithActor("operator-1", identity.PrincipalUser)
	_, err := ib.On(ctx, item.ID).
		Respond("approve", "Looks good").
		TransitionTo(inbox.StatusCompleted).
		Apply()

	// Apply must return an error because dispatch failed.
	require.Error(t, err)
	require.True(t, disp.called, "dispatcher should have been called")

	// Re-fetch the item to verify persisted state.
	fetched, err := ib.Get(ctx, item.ID)
	require.NoError(t, err)

	// Item must NOT be completed — still open.
	require.Equal(t, inbox.StatusOpen, fetched.Proto.Status,
		"item should remain open when dispatch fails")

	// But the response event IS persisted (phase 1 succeeded).
	// The transition event (ItemCompleted) must NOT be persisted.
	var hasResponded, hasCompleted bool
	for _, evt := range fetched.Proto.Events {
		if evt.DataType == "inbox.v1.ItemResponded" {
			hasResponded = true
		}
		if evt.DataType == "inbox.v1.ItemCompleted" {
			hasCompleted = true
		}
	}
	require.True(t, hasResponded,
		"response event should be persisted even when dispatch fails")
	require.False(t, hasCompleted,
		"completion event should NOT be persisted when dispatch fails")
}

func TestOp_TwoPhaseApply_DispatchSuccess(t *testing.T) {
	es := sharedEntityStore(t)
	disp := &mockDispatcher{err: nil}
	ib := inbox.New(es, inbox.WithDispatcher(disp))

	item := seedOpenItemWithCallback(t, ib, "dbos:wf-456")

	// Respond + TransitionTo completed with a dispatcher that succeeds.
	ctx := ctxWithActor("operator-2", identity.PrincipalUser)
	updated, err := ib.On(ctx, item.ID).
		Respond("approve", "All checks passed").
		TransitionTo(inbox.StatusCompleted).
		Apply()

	require.NoError(t, err)
	require.True(t, disp.called, "dispatcher should have been called")

	// Item should be completed.
	require.Equal(t, inbox.StatusCompleted, updated.Proto.Status)

	// Re-fetch to confirm persistence.
	fetched, err := ib.Get(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, inbox.StatusCompleted, fetched.Proto.Status)
}

func TestOp_SinglePhase_NoDispatcher(t *testing.T) {
	// No dispatcher — single-write path should work as before.
	ib := sharedInbox(t) // sharedInbox creates inbox without dispatcher
	item := seedOpenItemWithCallback(t, ib, "dbos:wf-789")

	ctx := ctxWithActor("operator-3", identity.PrincipalUser)
	updated, err := ib.On(ctx, item.ID).
		Respond("approve", "LGTM").
		TransitionTo(inbox.StatusCompleted).
		Apply()

	require.NoError(t, err)
	require.Equal(t, inbox.StatusCompleted, updated.Proto.Status)

	// Re-fetch to confirm persistence.
	fetched, err := ib.Get(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, inbox.StatusCompleted, fetched.Proto.Status)
}

