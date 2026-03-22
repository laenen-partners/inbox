package service

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/inbox/gen/inbox/v1/inboxv1connect"
	"github.com/laenen-partners/tags"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Handler implements the InboxService Connect-RPC service.
type Handler struct {
	ib *inbox.Inbox
}

var _ inboxv1connect.InboxServiceHandler = (*Handler)(nil)

// NewHandler creates an InboxService handler backed by the given inbox.
func NewHandler(ib *inbox.Inbox) *Handler {
	return &Handler{ib: ib}
}

func (h *Handler) GetItem(ctx context.Context, req *connect.Request[inboxv1.GetItemRequest]) (*connect.Response[inboxv1.GetItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	item, err := h.ib.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.GetItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) ListItems(ctx context.Context, req *connect.Request[inboxv1.ListItemsRequest]) (*connect.Response[inboxv1.ListItemsResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	var cursor *time.Time
	if req.Msg.Cursor != nil {
		t := req.Msg.Cursor.AsTime()
		cursor = &t
	}
	items, err := h.ib.ListByTags(ctx, req.Msg.Tags, inbox.ListOpts{
		PageSize: int(req.Msg.PageSize),
		Cursor:   cursor,
	})
	if err != nil {
		return nil, mapError(err)
	}
	resp := &inboxv1.ListItemsResponse{Items: toProtoSlice(items)}
	if int(req.Msg.PageSize) > 0 && len(items) == int(req.Msg.PageSize) {
		last := items[len(items)-1].UpdatedAt
		resp.NextCursor = timestamppb.New(last)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) SearchItems(ctx context.Context, req *connect.Request[inboxv1.SearchItemsRequest]) (*connect.Response[inboxv1.SearchItemsResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	items, err := h.ib.Search(ctx, req.Msg.Query, inbox.ListOpts{
		PageSize: int(req.Msg.PageSize),
	})
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.SearchItemsResponse{Items: toProtoSlice(items)}), nil
}

func (h *Handler) ClaimItem(ctx context.Context, req *connect.Request[inboxv1.ClaimItemRequest]) (*connect.Response[inboxv1.ClaimItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	actor := actorStr(ctx)
	item, err := h.ib.On(ctx, req.Msg.Id).
		TransitionTo(inbox.StatusClaimed).
		Tag(tags.Build("assignee", actor)).
		Apply()
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.ClaimItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) ReleaseItem(ctx context.Context, req *connect.Request[inboxv1.ReleaseItemRequest]) (*connect.Response[inboxv1.ReleaseItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	current, err := h.ib.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, mapError(err)
	}
	assignee := current.Tags.Value("assignee")
	op := h.ib.On(ctx, req.Msg.Id).TransitionTo(inbox.StatusOpen)
	if assignee != "" {
		op = op.Untag(tags.Build("assignee", assignee))
	}
	item, err := op.Apply()
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.ReleaseItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) RespondToItem(ctx context.Context, req *connect.Request[inboxv1.RespondToItemRequest]) (*connect.Response[inboxv1.RespondToItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	item, err := h.ib.Respond(ctx, req.Msg.Id, inbox.Response{
		Action:  req.Msg.Action,
		Comment: req.Msg.Comment,
	})
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.RespondToItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) CompleteItem(ctx context.Context, req *connect.Request[inboxv1.CompleteItemRequest]) (*connect.Response[inboxv1.CompleteItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	item, err := h.ib.Complete(ctx, req.Msg.Id)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.CompleteItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) CancelItem(ctx context.Context, req *connect.Request[inboxv1.CancelItemRequest]) (*connect.Response[inboxv1.CancelItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	item, err := h.ib.Cancel(ctx, req.Msg.Id, req.Msg.Reason)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.CancelItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) CommentOnItem(ctx context.Context, req *connect.Request[inboxv1.CommentOnItemRequest]) (*connect.Response[inboxv1.CommentOnItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	var opts *inbox.CommentOpts
	if len(req.Msg.Visibility) > 0 || len(req.Msg.Refs) > 0 {
		opts = &inbox.CommentOpts{Visibility: req.Msg.Visibility, Refs: req.Msg.Refs}
	}
	item, err := h.ib.Comment(ctx, req.Msg.Id, req.Msg.Body, opts)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.CommentOnItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) TagItem(ctx context.Context, req *connect.Request[inboxv1.TagItemRequest]) (*connect.Response[emptypb.Empty], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	if err := h.ib.Tag(ctx, req.Msg.Id, req.Msg.Tags...); err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *Handler) UntagItem(ctx context.Context, req *connect.Request[inboxv1.UntagItemRequest]) (*connect.Response[emptypb.Empty], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	if err := h.ib.Untag(ctx, req.Msg.Id, req.Msg.Tag); err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *Handler) RedispatchItem(ctx context.Context, req *connect.Request[inboxv1.RedispatchItemRequest]) (*connect.Response[inboxv1.RedispatchItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	if err := h.ib.Redispatch(ctx, req.Msg.Id); err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.RedispatchItemResponse{}), nil
}

// ─── Helpers ───

func actorStr(ctx context.Context) string {
	id := identity.MustFromContext(ctx)
	return string(id.PrincipalType()) + ":" + id.PrincipalID()
}

func mapError(err error) error {
	switch {
	case errors.Is(err, inbox.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, inbox.ErrInvalidTransition):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, inbox.ErrTerminalStatus):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
