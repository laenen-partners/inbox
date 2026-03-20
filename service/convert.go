package service

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func toProto(item inbox.Item) *inboxv1.InboxItem {
	return &inboxv1.InboxItem{
		Id:        item.ID,
		Data:      item.Proto,
		Tags:      item.Tags.Strings(),
		CreatedAt: timestamppb.New(item.CreatedAt),
		UpdatedAt: timestamppb.New(item.UpdatedAt),
	}
}

func toProtoSlice(items []inbox.Item) []*inboxv1.InboxItem {
	out := make([]*inboxv1.InboxItem, len(items))
	for i, item := range items {
		out[i] = toProto(item)
	}
	return out
}

func ctxWithIdentity(ctx context.Context, id *inboxv1.Identity) (context.Context, error) {
	if id == nil {
		return ctx, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("identity is required"))
	}
	pt := identity.PrincipalType(id.PrincipalType)
	if pt == "" {
		pt = identity.PrincipalUser
	}
	ident, err := identity.New(id.TenantId, id.WorkspaceId, id.PrincipalId, pt, id.Roles)
	if err != nil {
		return ctx, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return identity.WithContext(ctx, ident), nil
}
