package ui

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/laenen-partners/dsx/ds"
	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *server) handleClientRespond(w http.ResponseWriter, r *http.Request) {
	if s.cfg.verifier == nil {
		http.Error(w, "not configured", http.StatusNotFound)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	claims, err := s.cfg.verifier.Verify(r.Context(), token)
	if err != nil {
		http.Error(w, "invalid or expired link", http.StatusForbidden)
		return
	}

	item, err := s.ib.Get(r.Context(), claims.ItemID)
	if err != nil {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}

	var schema *inboxv1.ItemSchema
	if item.Proto.GetPayload() != nil {
		schema = tryParseSchema(item.PayloadType(), item.Proto.GetPayload().GetValue())
	}

	data := clientData{
		Item:     item,
		Schema:   schema,
		Token:    token,
		BasePath: s.cfg.basePath,
		Scope:    claims.Scope,
	}

	s.renderPage(w, r, "/respond", clientPage(data))
}

func (s *server) handleClientRespondSubmit(w http.ResponseWriter, r *http.Request) {
	if s.cfg.verifier == nil {
		http.Error(w, "not configured", http.StatusNotFound)
		return
	}

	// Read the token from signals
	var signals struct {
		Token string `json:"token"`
	}
	if err := ds.ReadSignals("client", r, &signals); err != nil {
		s.sseError(w, r, err)
		return
	}

	claims, err := s.cfg.verifier.Verify(r.Context(), signals.Token)
	if err != nil {
		s.sseError(w, r, fmt.Errorf("invalid or expired link"))
		return
	}

	if claims.Scope != inbox.ScopeRespond {
		s.sseError(w, r, fmt.Errorf("this link is view-only"))
		return
	}

	// Read the schema field values
	var schemaSignals map[string]interface{}
	var rawSignals map[string]json.RawMessage
	ds.ReadRaw(r, &rawSignals)
	if raw, ok := rawSignals["schema"]; ok {
		json.Unmarshal(raw, &schemaSignals)
	}

	// Build response comment from schema fields
	comment := "Submitted via presigned link"

	_, err = s.ib.Respond(r.Context(), claims.ItemID, inbox.Response{
		Actor:   claims.Actor,
		Action:  "submit",
		Comment: comment,
	})
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	// Complete the item
	_, err = s.ib.Complete(r.Context(), claims.ItemID, claims.Actor)
	if err != nil {
		// Item may already be completed or not claimable — just log
	}

	sse := datastar.NewSSE(w, r)
	ds.Send.Toast(sse, ds.ToastSuccess, "Response submitted. You can close this page.")
}
