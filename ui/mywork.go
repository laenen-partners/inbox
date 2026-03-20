package ui

import (
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/laenen-partners/tags"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleMyWork(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actor := actorStr(ctx)

	filterValues := s.readFilterValues(r)
	filterTags := []string{tags.Status(inbox.StatusClaimed), tags.Build("assignee", actor)}
	for _, f := range s.cfg.filters {
		if v := filterValues[filterKey(f.TagPrefix)]; v != "" {
			filterTags = append(filterTags, f.TagPrefix+v)
		}
	}

	var cursor *time.Time
	if c := r.URL.Query().Get("cursor"); c != "" {
		if t, err := time.Parse(time.RFC3339, c); err == nil {
			cursor = &t
		}
	}

	resp, err := s.client.ListItems(ctx, connect.NewRequest(&inboxv1.ListItemsRequest{
		Identity: identityToProto(ctx),
		Tags:     filterTags,
		PageSize: 50,
		Cursor:   cursorToProto(cursor),
	}))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := fromProtoSlice(resp.Msg.Items)

	data := queueData{
		Items:    items,
		Filters:  s.cfg.filters,
		BasePath: s.cfg.basePath,
	}
	if resp.Msg.NextCursor != nil {
		t := resp.Msg.NextCursor.AsTime()
		data.NextCursor = &t
	}

	// SSE fragment for Datastar filter/pagination, full page otherwise
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewSSE(w, r)
		_ = sse.PatchElementTempl(queueTable(data))
		return
	}

	s.renderPage(w, r, "/mywork", myworkContent(data))
}
