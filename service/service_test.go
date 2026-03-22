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
	return testClientWithOpts(t)
}

func testClientWithOpts(t *testing.T, opts ...service.Option) (inboxv1connect.InboxServiceClient, *inbox.Inbox) {
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
	client := service.NewLocalClient(service.NewHandler(ib, opts...))
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
	item, err := ib.Create(ctx, inbox.Meta{
		Title: title,
		Tags:  tags.MustNew("type:review", tags.Team("ops")),
	})
	require.NoError(t, err)
	return item
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

	// Verify assignee tag was set atomically
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

func TestGetItem_NotFound(t *testing.T) {
	client, _ := testClient(t)

	_, err := client.GetItem(context.Background(), connect.NewRequest(&inboxv1.GetItemRequest{
		Identity: testIdentity(),
		Id:       "nonexistent-id",
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestRespondCompletesOption(t *testing.T) {
	client, ib := testClientWithOpts(t, service.WithRespondCompletes())
	item := seedItem(t, ib, "Test respond completes")

	// Claim
	_, err := client.ClaimItem(context.Background(), connect.NewRequest(&inboxv1.ClaimItemRequest{
		Identity: testIdentity(),
		Id:       item.ID,
	}))
	require.NoError(t, err)

	// Respond — with WithRespondCompletes, this should also complete the item.
	resp, err := client.RespondToItem(context.Background(), connect.NewRequest(&inboxv1.RespondToItemRequest{
		Identity: testIdentity(),
		Id:       item.ID,
		Action:   "approve",
		Comment:  "Looks good",
	}))
	require.NoError(t, err)
	require.Equal(t, "completed", resp.Msg.Item.Data.Status)
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
