package inbox

import (
	"context"
	"time"
)

const (
	// ScopeRespond allows the token holder to respond to the item.
	ScopeRespond = "respond"
	// ScopeView allows the token holder to view the item (read-only).
	ScopeView = "view"
)

// TokenClaims carries the verified claims from a presigned token.
type TokenClaims struct {
	ItemID   string
	Actor    string
	Scope    string
	Exp      time.Time
	IssuedAt time.Time
}

// Signer creates presigned tokens for inbox items.
type Signer interface {
	Sign(ctx context.Context, itemID, actor, scope string, expiry time.Duration) (token string, exp time.Time, err error)
}

// Verifier validates presigned tokens and returns the claims.
type Verifier interface {
	Verify(ctx context.Context, token string) (*TokenClaims, error)
}
