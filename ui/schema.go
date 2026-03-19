package ui

import (
	"github.com/a-h/templ"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"google.golang.org/protobuf/proto"
)

// SchemaRenderer returns a PayloadRendererFunc that renders ItemSchema payloads
// using the generic schema template. Register it with WithPayloadRenderer for
// the ItemSchema type URL.
//
// Usage:
//
//	ui.Handler(ib,
//	    ui.WithPayloadRenderer("inbox.v1.ItemSchema", ui.SchemaRenderer()),
//	)
func SchemaRenderer() PayloadRendererFunc {
	return func(_ string, data []byte) templ.Component {
		var schema inboxv1.ItemSchema
		if err := proto.Unmarshal(data, &schema); err != nil {
			return nil
		}
		return schemaPayload(&schema)
	}
}
