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
