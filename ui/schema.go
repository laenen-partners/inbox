package ui

import (
	"encoding/json"

	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"google.golang.org/protobuf/proto"
)

// tryParseSchema attempts to unmarshal the payload as an ItemSchema.
// Returns nil if the payload type doesn't match or parsing fails.
func tryParseSchema(payloadType string, data []byte) *inboxv1.ItemSchema {
	if payloadType != "inbox.v1.ItemSchema" {
		return nil
	}
	var schema inboxv1.ItemSchema
	if err := proto.Unmarshal(data, &schema); err != nil {
		return nil
	}
	return &schema
}

// buildSchemaSignals builds a JSON string for Datastar data-signals
// from the schema's form fields, namespaced under "schema".
func buildSchemaSignals(schema *inboxv1.ItemSchema) string {
	fields := make(map[string]interface{})
	for _, f := range schema.Fields {
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
