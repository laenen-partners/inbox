package inbox_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
)

func TestRespond_ReturnsDispatchError(t *testing.T) {
	es := sharedEntityStore(t)
	disp := &mockDispatcher{err: fmt.Errorf("connection refused")}
	ib := inbox.New(es, inbox.WithDispatcher(disp))

	item := seedOpenItemWithCallback(t, ib, "dbos:wf-dispatch-err")

	ctx := ctxWithActor("operator-1", identity.PrincipalUser)
	_, err := ib.Respond(ctx, item.ID, inbox.Response{
		Action:  "approve",
		Comment: "Looks good",
	})

	// Respond must return an error because dispatch failed.
	require.Error(t, err)
	require.Contains(t, err.Error(), "dispatch failed")
	require.True(t, disp.called, "dispatcher should have been called")

	// Re-fetch the item to verify the response event IS persisted.
	fetched, err := ib.Get(ctx, item.ID)
	require.NoError(t, err)

	var hasResponded bool
	for _, evt := range fetched.Proto.Events {
		if evt.DataType == "inbox.v1.ItemResponded" {
			hasResponded = true
		}
	}
	require.True(t, hasResponded,
		"response event should be persisted even when dispatch fails")
}

func TestRedispatch_Success(t *testing.T) {
	es := sharedEntityStore(t)
	disp := &mockDispatcher{err: nil}
	ib := inbox.New(es, inbox.WithDispatcher(disp))

	item := seedOpenItemWithCallback(t, ib, "dbos:wf-redispatch")

	// First: respond to the item (dispatch succeeds).
	ctx := ctxWithActor("operator-1", identity.PrincipalUser)
	_, err := ib.Respond(ctx, item.ID, inbox.Response{
		Action:  "approve",
		Comment: "Looks good",
	})
	require.NoError(t, err)
	require.True(t, disp.called, "dispatcher should have been called on respond")

	// Reset the mock to detect the second call.
	disp.called = false

	// Redispatch should call the dispatcher again.
	err = ib.Redispatch(ctx, item.ID)
	require.NoError(t, err)
	require.True(t, disp.called, "dispatcher should have been called on redispatch")
}

func TestRedispatch_NoResponseEvent(t *testing.T) {
	es := sharedEntityStore(t)
	disp := &mockDispatcher{err: nil}
	ib := inbox.New(es, inbox.WithDispatcher(disp))

	// Create an item that has never been responded to.
	item := seedOpenItemWithCallback(t, ib, "dbos:wf-no-response")

	ctx := ctxWithActor("operator-1", identity.PrincipalUser)
	err := ib.Redispatch(ctx, item.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no response event")
	require.False(t, disp.called, "dispatcher should NOT have been called")
}
