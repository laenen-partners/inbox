package main

import (
	"context"

	"github.com/a-h/templ"
	"github.com/laenen-partners/inbox/schema"
	inboxui "github.com/laenen-partners/inbox/ui"
)

// schemaProvider implements ui.ContentProvider for ItemSchema payloads.
type schemaProvider struct{}

func (p schemaProvider) Render(ctx context.Context, rc inboxui.RenderContext) templ.Component {
	if rc.Item.Proto.GetPayload() == nil {
		return templ.NopComponent
	}
	s := schema.TryParse(rc.Item.PayloadType(), rc.Item.Proto.GetPayload().GetValue())
	if s == nil {
		return templ.NopComponent
	}
	return schema.Payload(s, rc.Item.ID, rc.BasePath)
}
