package inbox

import (
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// Item is the domain representation of an inbox item.
// It combines entity store fields (ID, Tags, CreatedAt, UpdatedAt) with
// the inbox-specific data stored in the entity's JSONB payload.
type Item struct {
	// ID is the entity store entity ID.
	ID string `json:"id"`

	// Optional idempotency key. When set, prevents duplicate creation
	// via entity store anchors. Derived from source context, e.g.
	// "workflow:onboarding-456:pep_screening".
	IdempotencyKey string `json:"idempotency_key,omitempty"`

	// Core fields (stored in entity data).
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Deadline    *time.Time `json:"deadline,omitempty"`

	// PayloadType is the fully qualified proto message name of the payload.
	// Example: "type.googleapis.com/forms.v1.ActionForm"
	// This is stored as a top-level JSONB field for easy querying
	// without parsing the payload itself.
	PayloadType string `json:"payload_type,omitempty"`

	// Typed payload serialized as google.protobuf.Any JSON.
	// Use PackPayload / UnpackPayload for type-safe access.
	Payload json.RawMessage `json:"payload,omitempty"`

	// Append-only activity log.
	Events []Event `json:"events,omitempty"`

	// Tags are stored in the entity store's tags column, not in JSONB.
	Tags []string `json:"-"`

	// Timestamps from the entity store.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Event records a single thing that happened on an inbox item.
type Event struct {
	At time.Time `json:"at"`

	// Who or what triggered this event.
	// Convention: "user:<id>", "agent:<name>", "workflow:<id>", "system".
	Actor string `json:"actor"`

	// Type is the fully qualified proto message name that identifies
	// what happened. Derived from the proto message, e.g.:
	//   "inbox.v1.ItemClaimed"
	//   "inbox.v1.CommentAppended"
	//   "compliance.v1.ScreeningResolved"
	Type string `json:"type"`

	// Human-readable detail or comment body.
	Detail string `json:"detail,omitempty"`

	// Structured event data serialized as JSON from the proto message.
	// Use UnpackEventData to deserialize into the concrete proto type.
	Data json.RawMessage `json:"data,omitempty"`
}

// Meta describes an inbox item to be created.
type Meta struct {
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Deadline       *time.Time      `json:"deadline,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	PayloadType    string          `json:"payload_type,omitempty"`
	Tags           []string        `json:"tags,omitempty"`
	Actor          string          `json:"actor"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

// Response is the data sent when responding to an item.
type Response struct {
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Comment string          `json:"comment,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// ListOpts configures list/search queries.
type ListOpts struct {
	PageSize int
	Cursor   *time.Time
}

// ─── Payload helpers ───

// PackPayload serializes a proto message into the Any JSON format
// used by Item.Payload. Returns both the JSON data and the type URL.
// The type URL should be stored in Item.PayloadType.
func PackPayload(msg proto.Message) (typeURL string, data json.RawMessage, err error) {
	a, err := anypb.New(msg)
	if err != nil {
		return "", nil, fmt.Errorf("inbox: pack payload: %w", err)
	}
	raw, err := protojson.Marshal(a)
	if err != nil {
		return "", nil, fmt.Errorf("inbox: marshal payload: %w", err)
	}
	return a.GetTypeUrl(), raw, nil
}

// UnpackPayload deserializes an Item.Payload (Any JSON) into a concrete
// proto message. Returns an error if the type doesn't match.
func UnpackPayload[T proto.Message](payload json.RawMessage, target T) error {
	if len(payload) == 0 {
		return nil
	}
	var a anypb.Any
	if err := protojson.Unmarshal(payload, &a); err != nil {
		return fmt.Errorf("inbox: unmarshal any: %w", err)
	}
	if err := a.UnmarshalTo(target); err != nil {
		return fmt.Errorf("inbox: unpack payload: %w", err)
	}
	return nil
}

// PackEventData serializes a proto message for use in Event.Data.
// Returns both the type URL (for Event.DataType) and the JSON data.
func PackEventData(msg proto.Message) (typeURL string, data json.RawMessage, err error) {
	return PackPayload(msg)
}

// UnpackEventData deserializes Event.Data into a concrete proto message.
func UnpackEventData[T proto.Message](data json.RawMessage, target T) error {
	return UnpackPayload(data, target)
}

// SetPayload is a convenience that packs a proto message and sets both
// PayloadType and Payload on a Meta.
func SetPayload(meta *Meta, msg proto.Message) error {
	typeURL, data, err := PackPayload(msg)
	if err != nil {
		return err
	}
	meta.PayloadType = typeURL
	meta.Payload = data
	return nil
}
