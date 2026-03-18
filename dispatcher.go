package inbox

import "context"

// Dispatcher delivers a response to the item's creator.
//
// Implementations handle a specific transport: webhooks, DBOS send,
// NATS publish, etc. The callback string comes from the item's
// "callback:<address>" tag.
//
// Dispatch is called best-effort after the response event has been
// recorded on the item. If dispatch fails, the response is still
// persisted — the caller can retry or handle the failure externally.
type Dispatcher interface {
	Dispatch(ctx context.Context, callback string, itemID string, resp Response) error
}
