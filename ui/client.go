package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/laenen-partners/dsx/ds"
	"github.com/laenen-partners/identity"
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

	// Render directly — bypass the app layout (no dashboard for client-facing pages)
	clientStandalonePage(data).Render(r.Context(), w)
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

	// Construct identity from token claims actor string (format "type:id")
	ctx := r.Context()
	pt := identity.PrincipalService
	pid := claims.Actor
	if i := strings.IndexByte(claims.Actor, ':'); i > 0 {
		prefix := claims.Actor[:i]
		pid = claims.Actor[i+1:]
		if prefix == string(identity.PrincipalUser) {
			pt = identity.PrincipalUser
		}
	}
	claimsID, _ := identity.New("token", "token", pid, pt, nil)
	ctx = identity.WithContext(ctx, claimsID)

	_, err = s.ib.Respond(ctx, claims.ItemID, inbox.Response{
		Action:  "submit",
		Comment: comment,
	})
	if err != nil {
		s.sseError(w, r, err)
		return
	}

	// Complete the item
	_, err = s.ib.Complete(ctx, claims.ItemID)
	if err != nil {
		// Item may already be completed or not claimable — just log
	}

	sse := datastar.NewSSE(w, r)
	ds.Send.Toast(sse, ds.ToastSuccess, "Response submitted. You can close this page.")
}
