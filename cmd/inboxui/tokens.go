package main

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	inboxtoken "github.com/laenen-partners/inbox/cmd/inboxui/token"
)

// HMACTokens implements token.Signer and token.Verifier using HMAC-SHA256 JWTs.
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

func (h *HMACTokens) Verify(_ context.Context, tokenStr string) (*inboxtoken.Claims, error) {
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

	result := &inboxtoken.Claims{
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
