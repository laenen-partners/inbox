package inbox

// Standard event data types. These match the proto definitions in
// proto/inbox/v1/events.proto and are used as typed event payloads
// throughout the inbox API.
//
// Every lifecycle method and convenience method on Inbox produces
// events with a data_type and structured data, making them queryable
// and deserializable by analytics systems.

// ─── Type URLs ───

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

// ─── Event data structs ───
// JSON-serializable structs matching the proto definitions.

// ItemCreated is emitted when an item is created.
type ItemCreated struct {
	PayloadType string `json:"payload_type,omitempty"`
}

// ItemClaimed is emitted when an actor claims an item.
type ItemClaimed struct {
	ClaimedBy string `json:"claimed_by"`
}

// ItemReleased is emitted when a claimed item is released back to open.
type ItemReleased struct {
	ReleasedBy string `json:"released_by"`
}

// ItemResponded is emitted when someone responds to an item.
type ItemResponded struct {
	Action         string `json:"action"`
	Comment        string `json:"comment,omitempty"`
	OnBehalfOf     string `json:"on_behalf_of,omitempty"`
	OverrideReason string `json:"override_reason,omitempty"`
}

// ItemCompleted is emitted when a workflow marks an item as completed.
type ItemCompleted struct {
	CompletedBy string `json:"completed_by"`
}

// ItemCancelled is emitted when an item is cancelled.
type ItemCancelled struct {
	CancelledBy string `json:"cancelled_by"`
	Reason      string `json:"reason,omitempty"`
}

// ItemExpired is emitted when a deadline-based expiry fires.
type ItemExpired struct{}

// CommentAppended is emitted when a comment is added.
type CommentAppended struct {
	Body       string   `json:"body"`
	Visibility []string `json:"visibility,omitempty"`
	Refs       []string `json:"refs,omitempty"`
}

// ItemEscalated is emitted when an item is escalated between teams.
type ItemEscalated struct {
	FromTeam string `json:"from_team"`
	ToTeam   string `json:"to_team"`
	Reason   string `json:"reason"`
}

// ItemReassigned is emitted when an item is reassigned between actors.
type ItemReassigned struct {
	FromActor string `json:"from_actor"`
	ToActor   string `json:"to_actor"`
	Reason    string `json:"reason"`
}

// TagsChanged is emitted when tags are modified.
type TagsChanged struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// PayloadUpdated is emitted when the item payload is replaced.
type PayloadUpdated struct {
	PayloadType string `json:"payload_type,omitempty"`
}
