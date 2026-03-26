package service_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/laenen-partners/entitystore"
	"github.com/laenen-partners/entitystore/store"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/inbox/gen/inbox/v1/inboxv1connect"
	"github.com/laenen-partners/inbox/service"
	"github.com/laenen-partners/tags"
)

var _sharedConnStr string

func testClient(t *testing.T) (inboxv1connect.InboxServiceClient, *inbox.Inbox) {
	t.Helper()
	ctx := context.Background()

	if _sharedConnStr == "" {
		pg, err := postgres.Run(ctx,
			"pgvector/pgvector:pg17",
			postgres.WithDatabase("service_test"),
			postgres.WithUsername("test"),
			postgres.WithPassword("test"),
			postgres.BasicWaitStrategies(),
		)
		require.NoError(t, err)
		connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
		require.NoError(t, err)
		pool, err := pgxpool.New(ctx, connStr)
		require.NoError(t, err)
		require.NoError(t, store.Migrate(ctx, pool))
		pool.Close()
		_sharedConnStr = connStr
	}

	pool, err := pgxpool.New(ctx, _sharedConnStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	es, err := entitystore.New(entitystore.WithPgStore(pool))
	require.NoError(t, err)

	ib := inbox.New(es)
	client := service.NewLocalClient(service.NewHandler(ib))
	return client, ib
}

func testIdentity() *inboxv1.Identity {
	return &inboxv1.Identity{
		TenantId:      "test",
		WorkspaceId:   "test",
		PrincipalId:   "operator",
		PrincipalType: "user",
	}
}

func seedItem(t *testing.T, ib *inbox.Inbox, title string) inbox.Item {
	t.Helper()
	id, _ := identity.New("test", "test", "seeder", identity.PrincipalService, nil)
	ctx := identity.WithContext(context.Background(), id)
	teamTag, _ := tags.Team("ops")
	item, err := ib.Create(ctx, inbox.Meta{
		Title: title,
		Tags:  tags.MustNew("type:review", teamTag),
	})
	require.NoError(t, err)
	return item
}

func TestCreateItem(t *testing.T) {
	client, _ := testClient(t)

	resp, err := client.CreateItem(context.Background(), connect.NewRequest(&inboxv1.CreateItemRequest{
		Identity:    testIdentity(),
		Title:       "New item via RPC",
		Description: "Created through the service layer",
		Tags:        []string{"type:approval", "team:finance"},
	}))
	require.NoError(t, err)
	require.NotEmpty(t, resp.Msg.Item.Id)
	require.Equal(t, "New item via RPC", resp.Msg.Item.Data.Title)
	require.Equal(t, "Created through the service layer", resp.Msg.Item.Data.Description)
	require.Equal(t, "open", resp.Msg.Item.Data.Status)
	require.Contains(t, resp.Msg.Item.Tags, "type:approval")
	require.Contains(t, resp.Msg.Item.Tags, "team:finance")
}

func TestGetItem(t *testing.T) {
	client, ib := testClient(t)
	item := seedItem(t, ib, "Test get")

	resp, err := client.GetItem(context.Background(), connect.NewRequest(&inboxv1.GetItemRequest{
		Identity: testIdentity(),
		Id:       item.ID,
	}))
	require.NoError(t, err)
	require.Equal(t, item.ID, resp.Msg.Item.Id)
	require.Equal(t, "Test get", resp.Msg.Item.Data.Title)
}

func TestGetItem_NotFound(t *testing.T) {
	client, _ := testClient(t)

	_, err := client.GetItem(context.Background(), connect.NewRequest(&inboxv1.GetItemRequest{
		Identity: testIdentity(),
		Id:       "nonexistent-id",
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestClaimAndRelease(t *testing.T) {
	client, ib := testClient(t)
	item := seedItem(t, ib, "Test claim")

	// Claim
	claimed, err := client.ClaimItem(context.Background(), connect.NewRequest(&inboxv1.ClaimItemRequest{
		Identity: testIdentity(),
		Id:       item.ID,
	}))
	require.NoError(t, err)
	require.Equal(t, "claimed", claimed.Msg.Item.Data.Status)

	// Verify assignee tag was set
	found := false
	for _, tag := range claimed.Msg.Item.Tags {
		if tag == "assignee:user:operator" {
			found = true
		}
	}
	require.True(t, found, "expected assignee tag, got %v", claimed.Msg.Item.Tags)

	// Release
	released, err := client.ReleaseItem(context.Background(), connect.NewRequest(&inboxv1.ReleaseItemRequest{
		Identity: testIdentity(),
		Id:       item.ID,
	}))
	require.NoError(t, err)
	require.Equal(t, "open", released.Msg.Item.Data.Status)

	// Verify assignee tag was removed
	for _, tag := range released.Msg.Item.Tags {
		require.NotContains(t, tag, "assignee:")
	}
}

func TestCloseItem(t *testing.T) {
	client, ib := testClient(t)
	item := seedItem(t, ib, "Test close")

	resp, err := client.CloseItem(context.Background(), connect.NewRequest(&inboxv1.CloseItemRequest{
		Identity: testIdentity(),
		Id:       item.ID,
		Reason:   "resolved externally",
	}))
	require.NoError(t, err)
	require.Equal(t, "closed", resp.Msg.Item.Data.Status)
}

func TestReassignItem(t *testing.T) {
	client, ib := testClient(t)
	item := seedItem(t, ib, "Test reassign")

	resp, err := client.ReassignItem(context.Background(), connect.NewRequest(&inboxv1.ReassignItemRequest{
		Identity:  testIdentity(),
		Id:        item.ID,
		FromActor: "",
		ToActor:   "user:alice",
		Reason:    "better suited",
	}))
	require.NoError(t, err)
	require.Equal(t, item.ID, resp.Msg.Item.Id)

	// Verify the reassignment event was recorded
	events := resp.Msg.Item.Data.Events
	lastEvt := events[len(events)-1]
	require.Equal(t, "inbox.v1.ItemReassigned", lastEvt.DataType)
}

func TestAddEvent(t *testing.T) {
	client, ib := testClient(t)
	item := seedItem(t, ib, "Test add event")

	resp, err := client.AddEvent(context.Background(), connect.NewRequest(&inboxv1.AddEventRequest{
		Identity: testIdentity(),
		Id:       item.ID,
		Detail:   "external system notification",
	}))
	require.NoError(t, err)
	require.Equal(t, item.ID, resp.Msg.Item.Id)

	// Verify the event was appended
	events := resp.Msg.Item.Data.Events
	require.GreaterOrEqual(t, len(events), 2) // created + the new event
	lastEvt := events[len(events)-1]
	require.Equal(t, "external system notification", lastEvt.Detail)
	require.Equal(t, "user:operator", lastEvt.Actor)
}

func TestIdentityRequired(t *testing.T) {
	client, ib := testClient(t)
	item := seedItem(t, ib, "Test identity")

	_, err := client.GetItem(context.Background(), connect.NewRequest(&inboxv1.GetItemRequest{
		Id: item.ID,
		// no identity
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
