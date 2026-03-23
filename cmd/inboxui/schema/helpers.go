package schema

import (
	"encoding/json"

	schemav1 "github.com/laenen-partners/inbox/cmd/inboxui/schema/gen/schema/v1"
	"google.golang.org/protobuf/proto"
)

// TryParse attempts to unmarshal the payload as an ItemSchema.
// Accepts both "schema.v1.ItemSchema" (current) and "inbox.v1.ItemSchema" (legacy).
func TryParse(payloadType string, data []byte) *schemav1.ItemSchema {
	if payloadType != "schema.v1.ItemSchema" && payloadType != "inbox.v1.ItemSchema" {
		return nil
	}
	var s schemav1.ItemSchema
	if err := proto.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

// BuildSignals builds a JSON string for Datastar data-signals
// from the schema's form fields, namespaced under "schema".
func BuildSignals(s *schemav1.ItemSchema) string {
	fields := make(map[string]interface{})
	for _, f := range s.Fields {
		if f.Type == "checkbox" {
			fields[f.Name] = f.DefaultValue == "true"
		} else {
			fields[f.Name] = f.DefaultValue
		}
	}
	wrapper := map[string]interface{}{"schema": fields}
	b, _ := json.Marshal(wrapper)
	return string(b)
}
