package main

import (
	"context"
	"time"

	"github.com/laenen-partners/inbox"
	"google.golang.org/protobuf/types/known/structpb"
)

func seedData(ctx context.Context, ib *inbox.Inbox) error {
	deadline := time.Now().Add(48 * time.Hour)

	items := []inbox.Meta{
		{
			Title:       "PEP screening review",
			Description: "Customer flagged as PEP during onboarding. Requires manual verification.",
			Tags:        []string{"type:review", "priority:urgent", "team:compliance"},
			Actor:       "workflow:onboarding-001",
			Deadline:    &deadline,
		},
		{
			Title:       "Invoice approval #INV-2026-0042",
			Description: "Invoice from supplier exceeds auto-approval threshold.",
			Tags:        []string{"type:approval", "priority:high", "team:finance"},
			Actor:       "workflow:invoice-processor",
		},
		{
			Title:       "Missing customer data",
			Description: "Onboarding form incomplete. Need phone number and address.",
			Tags:        []string{"type:input_required", "priority:normal", "team:ops"},
			Actor:       "agent:data-validator",
		},
		{
			Title:       "Contract terms review",
			Description: "Non-standard payment terms requested. Legal review needed.",
			Tags:        []string{"type:review", "priority:normal", "team:compliance"},
			Actor:       "workflow:contract-review",
		},
		{
			Title:       "Expense report approval",
			Description: "Travel expenses submitted for Q1 conference.",
			Tags:        []string{"type:approval", "priority:low", "team:finance"},
			Actor:       "system",
		},
	}

	for _, meta := range items {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"source": meta.Actor,
			"note":   "Seeded test data",
		})
		meta.Payload = payload
		if _, err := ib.Create(ctx, meta); err != nil {
			return err
		}
	}
	return nil
}
