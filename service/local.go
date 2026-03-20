package service

import (
	"net/http"
	"net/http/httptest"

	"connectrpc.com/connect"
	"github.com/laenen-partners/inbox/gen/inbox/v1/inboxv1connect"
)

// localRoundTripper routes HTTP requests to a handler in-process.
type localRoundTripper struct {
	handler http.Handler
}

func (t localRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

// NewLocalClient creates an in-process InboxServiceClient backed by the given handler.
func NewLocalClient(h *Handler, opts ...connect.HandlerOption) inboxv1connect.InboxServiceClient {
	path, handler := inboxv1connect.NewInboxServiceHandler(h, opts...)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	return inboxv1connect.NewInboxServiceClient(
		&http.Client{Transport: localRoundTripper{handler: mux}},
		"http://in-process",
	)
}
