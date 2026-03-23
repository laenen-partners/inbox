package token

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/laenen-partners/dsx/ds"
	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	"github.com/laenen-partners/inbox/cmd/inboxui/schema"
	schemav1 "github.com/laenen-partners/inbox/cmd/inboxui/schema/gen/schema/v1"
	"github.com/starfederation/datastar-go/datastar"
)

// Handler serves the client-facing presigned link endpoint (GET + POST /respond).
type Handler struct {
	inbox    *inbox.Inbox
	verifier Verifier
}

// NewHandler creates a new presigned link handler.
func NewHandler(ib *inbox.Inbox, v Verifier) *Handler {
	return &Handler{inbox: ib, verifier: v}
}

// ServeHTTP dispatches GET and POST for the /respond endpoint.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodPost:
		h.handlePost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	claims, err := h.verifier.Verify(r.Context(), tok)
	if err != nil {
		http.Error(w, "invalid or expired link", http.StatusForbidden)
		return
	}

	item, err := h.inbox.Get(r.Context(), claims.ItemID)
	if err != nil {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}

	var sch *schemav1.ItemSchema
	if item.Proto.GetPayload() != nil {
		sch = schema.TryParse(item.PayloadType(), item.Proto.GetPayload().GetValue())
	}

	data := clientData{
		Item:   item,
		Schema: sch,
		Token:  tok,
		Scope:  claims.Scope,
	}

	// Render directly — bypass the app layout (no dashboard for client-facing pages)
	_ = clientStandalonePage(data).Render(r.Context(), w)
}

func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request) {
	// Read the token from signals
	var signals struct {
		Token string `json:"token"`
	}
	if err := ds.ReadSignals("client", r, &signals); err != nil {
		sseError(w, r, err)
		return
	}

	claims, err := h.verifier.Verify(r.Context(), signals.Token)
	if err != nil {
		sseError(w, r, fmt.Errorf("invalid or expired link"))
		return
	}

	if claims.Scope != ScopeRespond {
		sseError(w, r, fmt.Errorf("this link is view-only"))
		return
	}

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

	_, err = h.inbox.Close(ctx, claims.ItemID, "submitted via presigned link")
	if err != nil && !errors.Is(err, inbox.ErrTerminalStatus) {
		sseError(w, r, err)
		return
	}

	sse := datastar.NewSSE(w, r)
	_ = ds.Send.Toast(sse, ds.ToastSuccess, "Response submitted. You can close this page.")
}

func sseError(w http.ResponseWriter, r *http.Request, err error) {
	sse := datastar.NewSSE(w, r)
	_ = ds.Send.Toast(sse, ds.ToastError, err.Error())
}
