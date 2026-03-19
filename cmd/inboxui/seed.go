package main

import (
	"context"
	"time"

	"github.com/laenen-partners/inbox"
	"google.golang.org/protobuf/types/known/structpb"
)

func seedData(ctx context.Context, ib *inbox.Inbox) error {
	deadline := time.Now().Add(48 * time.Hour)

	// Address input item
	addressPayload, _ := structpb.NewStruct(map[string]interface{}{
		"_type":   "address_request",
		"message": "Please provide the customer's registered business address for KYC verification.",
		"street":  "",
		"city":    "",
		"zip":     "",
		"country": "",
	})
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Missing customer address",
		Description: "Onboarding form incomplete. Business address required for KYC verification.",
		Tags:        []string{"type:input_required", "priority:normal", "team:ops"},
		Actor:       "agent:data-validator",
		Payload:     addressPayload,
	}); err != nil {
		return err
	}

	// Consent approval item
	consentPayload, _ := structpb.NewStruct(map[string]interface{}{
		"_type": "consent_request",
		"items": []interface{}{
			map[string]interface{}{
				"name":        "Data Processing Agreement",
				"description": "Consent to process personal data under GDPR Article 6(1)(a).",
				"required":    true,
			},
			map[string]interface{}{
				"name":        "Marketing Communications",
				"description": "Opt-in to receive product updates and promotional emails.",
				"required":    false,
			},
			map[string]interface{}{
				"name":        "Third-Party Data Sharing",
				"description": "Allow sharing anonymized data with analytics partners.",
				"required":    false,
			},
		},
	})
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Customer consent review",
		Description: "New customer onboarding requires consent verification for 3 items.",
		Tags:        []string{"type:approval", "priority:high", "team:compliance"},
		Actor:       "workflow:onboarding-001",
		Deadline:    &deadline,
		Payload:     consentPayload,
	}); err != nil {
		return err
	}

	// Multi-choice item
	multiChoicePayload, _ := structpb.NewStruct(map[string]interface{}{
		"_type":          "multi_choice",
		"question":       "What payment terms should apply to this customer?",
		"allow_multiple": false,
		"options": []interface{}{
			"Net 30 (standard)",
			"Net 60 (extended)",
			"Net 90 (enterprise)",
			"Prepayment required",
		},
		"note": "Customer has requested Net 60 but their credit score suggests Net 30.",
	})
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Payment terms decision",
		Description: "Non-standard payment terms requested. Select appropriate terms based on credit assessment.",
		Tags:        []string{"type:review", "priority:normal", "team:finance"},
		Actor:       "workflow:contract-review",
		Payload:     multiChoicePayload,
	}); err != nil {
		return err
	}

	// Multi-select item
	multiSelectPayload, _ := structpb.NewStruct(map[string]interface{}{
		"_type":          "multi_choice",
		"question":       "Which verification documents were provided?",
		"allow_multiple": true,
		"options": []interface{}{
			"Government-issued ID",
			"Proof of address (utility bill)",
			"Bank statement",
			"Company registration certificate",
			"Tax identification number",
		},
		"note": "At least 2 documents required for enhanced due diligence.",
	})
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "PEP screening review",
		Description: "Customer flagged as PEP during onboarding. Requires manual verification of provided documents.",
		Tags:        []string{"type:review", "priority:urgent", "team:compliance"},
		Actor:       "workflow:onboarding-001",
		Deadline:    &deadline,
		Payload:     multiSelectPayload,
	}); err != nil {
		return err
	}

	// Pre-filled address item
	prefilledAddress, _ := structpb.NewStruct(map[string]interface{}{
		"_type":   "address_request",
		"message": "Verify the supplier's billing address matches their invoice.",
		"street":  "123 Commerce Street, Suite 400",
		"city":    "Amsterdam",
		"zip":     "1012 AB",
		"country": "Netherlands",
	})
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Invoice approval #INV-2026-0042",
		Description: "Invoice from supplier exceeds auto-approval threshold. Verify billing address.",
		Tags:        []string{"type:approval", "priority:high", "team:finance"},
		Actor:       "workflow:invoice-processor",
		Payload:     prefilledAddress,
	}); err != nil {
		return err
	}

	// Generic payload items (will show as JSON)
	genericPayload, _ := structpb.NewStruct(map[string]interface{}{
		"source": "system",
		"note":   "Travel expenses submitted for Q1 conference",
		"amount": 2450.00,
	})
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Expense report approval",
		Description: "Travel expenses submitted for Q1 conference.",
		Tags:        []string{"type:approval", "priority:low", "team:finance"},
		Actor:       "system",
		Payload:     genericPayload,
	}); err != nil {
		return err
	}

	return nil
}
