# Token System Design

## Overview

Pluggable token signing and verification for presigned inbox item links. The inbox library defines interfaces (`Signer`, `Verifier`) and a `TokenClaims` struct. Consuming apps provide their own implementation (HMAC, RSA, KMS). The standalone binary ships an HMAC-SHA256 JWT implementation for demo purposes.

Also introduces a context-based actor accessor (`WithActor`/`ActorFrom`) so all consumers (UI handlers, token middleware, background jobs) share the same mechanism.

## Token interfaces

```go
package inbox

type TokenClaims struct {
    ItemID string
    Actor  string
    Scope  string        // "respond", "view"
    Exp    time.Time
}

type Signer interface {
    Sign(itemID, actor, scope string, expiry time.Duration) (string, error)
}

type Verifier interface {
    Verify(token string) (*TokenClaims, error)
}
```

The library defines only the interfaces and claims struct. Implementations are pluggable — the consuming app provides its own `Signer`/`Verifier`.

Expiry is `time.Duration` passed at sign time. The signer computes `exp = time.Now().Add(expiry)`. This lets callers control TTL per-token (e.g. 24h for email links, 1h for SMS OTPs).

## Actor from context

Context-based actor accessor in the inbox library:

```go
package inbox

func WithActor(ctx context.Context, actor string) context.Context
func ActorFrom(ctx context.Context) string
```

All HTTP-layer consumers use this instead of ad-hoc context keys:
- The UI's actor middleware calls `inbox.WithActor(ctx, actorFn(r))` and stores the result in context
- Token verification middleware calls `inbox.WithActor(ctx, claims.Actor)`
- UI handlers call `inbox.ActorFrom(ctx)` to get the actor

The UI's `WithActor` config option signature stays the same (`func(r *http.Request) string`). The middleware implementation changes to use `inbox.WithActor`/`inbox.ActorFrom` instead of a UI-internal context key.

No changes to inbox backend API signatures — `Claim`, `Respond`, etc. still take explicit actor strings. `ActorFrom` is a convenience for HTTP handlers.

## HMAC implementation (standalone binary)

The standalone binary provides a concrete implementation using `golang-jwt/jwt` (HS256):

```go
type HMACTokens struct {
    secret []byte
}
```

Implements both `Signer` and `Verifier`. JWT claims:
- `sub`: actor
- `item_id`: inbox item ID
- `scope`: "respond" or "view"
- `exp`: Unix timestamp

Uses the same 32-byte random secret already generated for CSRF, or a separate configurable one.

## File layout

```
inbox/
  actor.go          — WithActor(ctx), ActorFrom(ctx)
  token.go          — TokenClaims, Signer, Verifier interfaces

  ui/
    handler.go      — middleware uses inbox.WithActor/ActorFrom (replaces internal ctxKey)
    config.go       — WithActor option unchanged (still func(r) string)

  cmd/inboxui/
    tokens.go       — HMACTokens implementation using golang-jwt/jwt
    main.go         — wires HMACTokens
```

The UI handler does not know about `Signer`/`Verifier`. Token verification is app-level middleware. The UI just reads `inbox.ActorFrom(ctx)`.

## Dependencies

- `github.com/golang-jwt/jwt/v5` — added to `go.mod` for the standalone binary's HMAC implementation
