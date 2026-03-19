# Token System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add pluggable token signing/verification interfaces and a shared context-based actor accessor to the inbox library.

**Architecture:** The inbox library defines `Signer`/`Verifier` interfaces, `TokenClaims` struct, scope constants, and `WithActor`/`ActorFrom` context helpers. The UI refactors to use the shared actor context. The standalone binary provides an HMAC-JWT implementation.

**Tech Stack:** Go, golang-jwt/jwt/v5

**Spec:** `docs/superpowers/specs/2026-03-19-token-system-design.md`

---

## File Structure

```
inbox/
  actor.go          — WithActor(ctx), ActorFrom(ctx) context helpers
  actor_test.go     — Unit tests for actor context helpers
  token.go          — TokenClaims, Signer, Verifier interfaces, scope constants

  ui/
    handler.go      — Refactor: use inbox.WithActor/ActorFrom, remove internal ctxKey

  cmd/inboxui/
    tokens.go       — HMACTokens implementation (Signer + Verifier)
    tokens_test.go  — Unit tests for HMACTokens
    main.go         — Wire HMACTokens, update actor middleware
```

---

## Task 1: Actor context helpers

**Files:**
- Create: `actor.go`
- Create: `actor_test.go`

- [ ] **Step 1: Write tests**

```go
package inbox

import (
	"context"
	"testing"
)

func TestActorContext(t *testing.T) {
	ctx := context.Background()

	// Empty context returns ""
	if got := ActorFrom(ctx); got != "" {
		t.Errorf("ActorFrom(empty) = %q, want empty", got)
	}

	// Set and retrieve
	ctx = WithActor(ctx, "user:operator")
	if got := ActorFrom(ctx); got != "user:operator" {
		t.Errorf("ActorFrom = %q, want %q", got, "user:operator")
	}

	// Override
	ctx = WithActor(ctx, "user:admin")
	if got := ActorFrom(ctx); got != "user:admin" {
		t.Errorf("ActorFrom = %q, want %q", got, "user:admin")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestActorContext ./...`
Expected: FAIL (functions not defined)

- [ ] **Step 3: Implement**

```go
package inbox

import "context"

type actorCtxKey struct{}

// WithActor stores an actor identifier in the context.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, actor)
}

// ActorFrom extracts the actor from the context. Returns "" if not set.
func ActorFrom(ctx context.Context) string {
	if v, ok := ctx.Value(actorCtxKey{}).(string); ok {
		return v
	}
	return ""
}
```

- [ ] **Step 4: Run tests**

Run: `go test -v -run TestActorContext ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add actor.go actor_test.go
git commit -m "feat: add WithActor/ActorFrom context helpers"
```

---

## Task 2: Token interfaces and scope constants

**Files:**
- Create: `token.go`

- [ ] **Step 1: Create `token.go`**

```go
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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add token.go
git commit -m "feat: add Signer/Verifier interfaces and TokenClaims"
```

---

## Task 3: Refactor UI to use inbox.WithActor/ActorFrom

**Files:**
- Modify: `ui/handler.go`

This is a mechanical refactor. Replace the UI's internal `ctxKey`/`actorFrom` with `inbox.WithActor`/`inbox.ActorFrom`. All 9 call sites in `ui/actions.go`, `ui/detail.go`, `ui/mywork.go` use `actorFrom(ctx)` which becomes `inbox.ActorFrom(ctx)`.

No changes needed in `ui/config.go` — the `WithActor` option and `actorFn` field are unchanged.

Note: The fallback behavior changes from `"anonymous"` (old `actorFrom`) to `""` (`inbox.ActorFrom`). This is by design — the `actorMiddleware` always sets the actor via `actorFn`, so UI routes never hit the empty-string path.

- [ ] **Step 1: Update `ui/handler.go`**

Remove:
- `type ctxKey string`
- `const actorKey ctxKey = "inbox-ui-actor"`
- `func actorFrom(ctx context.Context) string { ... }`
- The `"context"` import (becomes unused after removing `context.WithValue` call)

Update `actorMiddleware` to use `inbox.WithActor`:
```go
func (s *server) actorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := s.cfg.actorFn(r)
		ctx := inbox.WithActor(r.Context(), actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

- [ ] **Step 2: Replace all `actorFrom(ctx)` calls with `inbox.ActorFrom(ctx)`**

Files to update (use find-and-replace):
- `ui/actions.go` — 7 call sites
- `ui/detail.go` — 1 call site
- `ui/mywork.go` — 1 call site

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `go build ./... && go test -v ./ui/`
Expected: SUCCESS, all tests PASS

- [ ] **Step 4: Commit**

```bash
git add ui/handler.go ui/actions.go ui/detail.go ui/mywork.go
git commit -m "refactor(ui): use inbox.WithActor/ActorFrom instead of internal context key"
```

---

## Task 4: HMAC JWT implementation

**Files:**
- Create: `cmd/inboxui/tokens.go`
- Create: `cmd/inboxui/tokens_test.go`
- Modify: `go.mod` (add golang-jwt dependency)

- [ ] **Step 1: Add dependency**

```bash
go get github.com/golang-jwt/jwt/v5@latest
```

- [ ] **Step 2: Write tests**

```go
package main

import (
	"context"
	"testing"
	"time"

	"github.com/laenen-partners/inbox"
)

func TestHMACTokens(t *testing.T) {
	tokens := NewHMACTokens([]byte("test-secret-key-at-least-32-bytes!"))
	ctx := context.Background()

	t.Run("sign_and_verify", func(t *testing.T) {
		token, exp, err := tokens.Sign(ctx, "item-123", "user:alice", inbox.ScopeRespond, time.Hour)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if token == "" {
			t.Fatal("empty token")
		}
		if exp.Before(time.Now()) {
			t.Error("exp should be in the future")
		}

		claims, err := tokens.Verify(ctx, token)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if claims.ItemID != "item-123" {
			t.Errorf("ItemID = %q, want %q", claims.ItemID, "item-123")
		}
		if claims.Actor != "user:alice" {
			t.Errorf("Actor = %q, want %q", claims.Actor, "user:alice")
		}
		if claims.Scope != inbox.ScopeRespond {
			t.Errorf("Scope = %q, want %q", claims.Scope, inbox.ScopeRespond)
		}
		if claims.IssuedAt.IsZero() {
			t.Error("IssuedAt should be set")
		}
	})

	t.Run("expired_token", func(t *testing.T) {
		token, _, err := tokens.Sign(ctx, "item-456", "user:bob", inbox.ScopeView, -time.Hour)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}

		_, err = tokens.Verify(ctx, token)
		if err == nil {
			t.Fatal("expected error for expired token")
		}
	})

	t.Run("invalid_token", func(t *testing.T) {
		_, err := tokens.Verify(ctx, "garbage.token.data")
		if err == nil {
			t.Fatal("expected error for invalid token")
		}
	})

	t.Run("wrong_secret", func(t *testing.T) {
		other := NewHMACTokens([]byte("different-secret-also-32-bytes!!"))
		token, _, _ := tokens.Sign(ctx, "item-789", "user:eve", inbox.ScopeRespond, time.Hour)

		_, err := other.Verify(ctx, token)
		if err == nil {
			t.Fatal("expected error for wrong secret")
		}
	})
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -v -run TestHMACTokens ./cmd/inboxui/`
Expected: FAIL (NewHMACTokens not defined)

- [ ] **Step 4: Implement**

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/laenen-partners/inbox"
)

// HMACTokens implements inbox.Signer and inbox.Verifier using HMAC-SHA256 JWTs.
type HMACTokens struct {
	secret []byte
}

// NewHMACTokens creates a new HMAC token signer/verifier.
func NewHMACTokens(secret []byte) *HMACTokens {
	return &HMACTokens{secret: secret}
}

func (h *HMACTokens) Sign(_ context.Context, itemID, actor, scope string, expiry time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(expiry)

	claims := jwt.MapClaims{
		"sub":     actor,
		"item_id": itemID,
		"scope":   scope,
		"exp":     jwt.NewNumericDate(exp),
		"iat":     jwt.NewNumericDate(now),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(h.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

func (h *HMACTokens) Verify(_ context.Context, tokenStr string) (*inbox.TokenClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return h.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	exp, _ := claims.GetExpirationTime()
	iat, _ := claims.GetIssuedAt()

	itemID, _ := claims["item_id"].(string)
	actor, _ := claims["sub"].(string)
	scope, _ := claims["scope"].(string)
	if itemID == "" || actor == "" {
		return nil, fmt.Errorf("token missing required claims")
	}

	result := &inbox.TokenClaims{
		ItemID: itemID,
		Actor:  actor,
		Scope:  scope,
	}
	if exp != nil {
		result.Exp = exp.Time
	}
	if iat != nil {
		result.IssuedAt = iat.Time
	}
	return result, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test -v -run TestHMACTokens ./cmd/inboxui/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/inboxui/tokens.go cmd/inboxui/tokens_test.go go.mod go.sum
git commit -m "feat: add HMAC JWT token implementation"
```

---

## Task 5: Wire tokens into standalone binary

**Files:**
- Modify: `cmd/inboxui/main.go`

- [ ] **Step 1: Update main.go**

Update the actor middleware to check context first (for token-set actors):

```go
inboxui.WithActor(func(r *http.Request) string {
    // Check if actor was already set (e.g. by token middleware)
    if actor := inbox.ActorFrom(r.Context()); actor != "" {
        return actor
    }
    if actor := r.URL.Query().Get("actor"); actor != "" {
        return actor
    }
    return "user:operator"
}),
```

Create the `HMACTokens` instance using the same secret:

```go
tokens := NewHMACTokens(secret)
_ = tokens // available for future token middleware
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/inboxui/`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add cmd/inboxui/main.go
git commit -m "feat: wire HMACTokens into standalone binary"
```

---

## Task 6: Verify full integration

- [ ] **Step 1: Run all tests**

Run: `go test -v -count=1 -timeout 120s ./...`
Expected: All PASS

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Build everything**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 4: Final commit (if any cleanup needed)**
