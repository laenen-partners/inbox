package main

import (
	"context"

	"github.com/a-h/templ"
	"github.com/laenen-partners/inbox"
	"github.com/laenen-partners/inbox/cmd/inboxui/schema"
	inboxui "github.com/laenen-partners/inbox/ui"
)

// schemaProvider implements ui.ContentProvider for ItemSchema payloads.
// It renders schema display/form fields and a Submit button that posts
// to the inbox UI's close endpoint when the item is completable.
type schemaProvider struct{}

func (p schemaProvider) Render(ctx context.Context, rc inboxui.RenderContext) templ.Component {
	if rc.Item.Proto.GetPayload() == nil {
		return templ.NopComponent
	}
	s := schema.TryParse(rc.Item.PayloadType(), rc.Item.Proto.GetPayload().GetValue())
	if s == nil {
		return templ.NopComponent
	}

	pc := schema.PayloadContext{
		ItemID:   rc.Item.ID,
		BasePath: rc.BasePath,
		Status:   rc.Item.Status(),
	}

	// Show submit button when item is not terminal and schema allows client completion.
	if !inbox.IsTerminal(rc.Item.Status()) && s.ClientCompletable {
		pc.SubmitURL = rc.BasePath + "/items/" + rc.Item.ID + "/close"
	}

	return schema.Payload(s, pc)
}
