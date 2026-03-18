package inbox

import (
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
)

// Re-exported generated proto event types for convenience.
// These are the well-known event data types produced by inbox lifecycle
// and activity methods. Use them directly when constructing or reading events.
type (
	ItemCreated     = inboxv1.ItemCreated
	ItemClaimed     = inboxv1.ItemClaimed
	ItemReleased    = inboxv1.ItemReleased
	ItemResponded   = inboxv1.ItemResponded
	ItemCompleted   = inboxv1.ItemCompleted
	ItemCancelled   = inboxv1.ItemCancelled
	ItemExpired     = inboxv1.ItemExpired
	CommentAppended = inboxv1.CommentAppended
	ItemEscalated   = inboxv1.ItemEscalated
	ItemReassigned  = inboxv1.ItemReassigned
	TagsChanged     = inboxv1.TagsChanged
	PayloadUpdated  = inboxv1.PayloadUpdated
)

// Type URL constants for event data_type fields.
// These match the fully qualified proto message names.
const (
	TypeItemCreated     = "inbox.v1.ItemCreated"
	TypeItemClaimed     = "inbox.v1.ItemClaimed"
	TypeItemReleased    = "inbox.v1.ItemReleased"
	TypeItemResponded   = "inbox.v1.ItemResponded"
	TypeItemCompleted   = "inbox.v1.ItemCompleted"
	TypeItemCancelled   = "inbox.v1.ItemCancelled"
	TypeItemExpired     = "inbox.v1.ItemExpired"
	TypeCommentAppended = "inbox.v1.CommentAppended"
	TypeItemEscalated   = "inbox.v1.ItemEscalated"
	TypeItemReassigned  = "inbox.v1.ItemReassigned"
	TypeTagsChanged     = "inbox.v1.TagsChanged"
	TypePayloadUpdated  = "inbox.v1.PayloadUpdated"
)
