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

func (h *Handler) CreateItem(ctx context.Context, req *connect.Request[inboxv1.CreateItemRequest]) (*connect.Response[inboxv1.CreateItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}

	meta := inbox.Meta{
		Title:          req.Msg.Title,
		Description:    req.Msg.Description,
		IdempotencyKey: req.Msg.IdempotencyKey,
	}
	if req.Msg.Deadline != nil {
		t := req.Msg.Deadline.AsTime()
		meta.Deadline = &t
	}
	if req.Msg.Payload != nil {
		meta.PayloadAny = req.Msg.Payload
		meta.PayloadTypeName = req.Msg.PayloadType
	}
	if len(req.Msg.Tags) > 0 {
		meta.Tags = tags.FromStrings(req.Msg.Tags)
	}

	item, err := h.ib.Create(ctx, meta)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.CreateItemResponse{Item: toProto(item)}), nil
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
	item, err := h.ib.Claim(ctx, req.Msg.Id)
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
	item, err := h.ib.Release(ctx, req.Msg.Id)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.ReleaseItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) ReassignItem(ctx context.Context, req *connect.Request[inboxv1.ReassignItemRequest]) (*connect.Response[inboxv1.ReassignItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	item, err := h.ib.Reassign(ctx, req.Msg.Id, req.Msg.FromActor, req.Msg.ToActor, req.Msg.Reason)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.ReassignItemResponse{Item: toProto(item)}), nil
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

func (h *Handler) AddEvent(ctx context.Context, req *connect.Request[inboxv1.AddEventRequest]) (*connect.Response[inboxv1.AddEventResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	evt := &inboxv1.Event{
		Actor:    actorStr(ctx),
		Detail:   req.Msg.Detail,
		DataType: req.Msg.DataType,
		Data:     req.Msg.Data,
	}
	item, err := h.ib.AddEvent(ctx, req.Msg.Id, evt)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.AddEventResponse{Item: toProto(item)}), nil
}

func (h *Handler) CloseItem(ctx context.Context, req *connect.Request[inboxv1.CloseItemRequest]) (*connect.Response[inboxv1.CloseItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	item, err := h.ib.Close(ctx, req.Msg.Id, req.Msg.Reason)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.CloseItemResponse{Item: toProto(item)}), nil
}

func (h *Handler) TagItem(ctx context.Context, req *connect.Request[inboxv1.TagItemRequest]) (*connect.Response[inboxv1.TagItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	if err := h.ib.Tag(ctx, req.Msg.Id, req.Msg.Tags...); err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.TagItemResponse{}), nil
}

func (h *Handler) UntagItem(ctx context.Context, req *connect.Request[inboxv1.UntagItemRequest]) (*connect.Response[inboxv1.UntagItemResponse], error) {
	ctx, err := ctxWithIdentity(ctx, req.Msg.Identity)
	if err != nil {
		return nil, err
	}
	if err := h.ib.Untag(ctx, req.Msg.Id, req.Msg.Tag); err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&inboxv1.UntagItemResponse{}), nil
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
