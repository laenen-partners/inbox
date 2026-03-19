package main

import (
	"context"
	"time"

	"github.com/laenen-partners/inbox"
	inboxv1 "github.com/laenen-partners/inbox/gen/inbox/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func seedData(ctx context.Context, ib *inbox.Inbox) error {
	deadline := time.Now().Add(48 * time.Hour)

	// --- Schema-based items (using ItemSchema proto) ---

	// Address collection
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Collect customer address",
		Description: "Residential address required for KYC verification.",
		Tags:        []string{"type:input_required", "priority:normal", "team:ops"},
		Actor:       "workflow:onboarding-001",
		Payload: &inboxv1.ItemSchema{
			Display: []*inboxv1.DisplayField{
				{Label: "Customer", Value: "CUST-1234"},
				{Label: "Product", Value: "Current Account — AED"},
				{Label: "Subscription", Value: "SUB-2026-0042", Mono: true},
			},
			Fields: []*inboxv1.FormField{
				{Name: "street", Type: "text", Label: "Street", Placeholder: "123 Main Street", Required: true},
				{Name: "city", Type: "text", Label: "City", Placeholder: "Amsterdam", Required: true},
				{Name: "zip", Type: "text", Label: "ZIP / Postal Code", Placeholder: "1012 AB", Required: true},
				{Name: "country", Type: "select", Label: "Country", Options: []string{"Netherlands", "Belgium", "Germany", "France", "United Kingdom"}, Required: true},
			},
			Actions: []*inboxv1.Action{
				{Name: "submit", Label: "Submit Address", Variant: "success"},
			},
			ClientCompletable: true,
		},
	}); err != nil {
		return err
	}

	// Consent collection
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Customer consent review",
		Description: "New customer onboarding requires consent verification for 3 items.",
		Tags:        []string{"type:approval", "priority:high", "team:compliance"},
		Actor:       "workflow:onboarding-001",
		Deadline:    &deadline,
		Payload: &inboxv1.ItemSchema{
			Display: []*inboxv1.DisplayField{
				{Label: "Customer", Value: "CUST-1234"},
				{Label: "Onboarding", Value: "workflow:onboarding-001", Mono: true},
			},
			Fields: []*inboxv1.FormField{
				{Name: "dpa", Type: "checkbox", Label: "Data Processing Agreement", Description: "Consent to process personal data under GDPR Article 6(1)(a).", Required: true},
				{Name: "marketing", Type: "checkbox", Label: "Marketing Communications", Description: "Opt-in to receive product updates and promotional emails."},
				{Name: "third_party", Type: "checkbox", Label: "Third-Party Data Sharing", Description: "Allow sharing anonymized data with analytics partners."},
			},
			Actions: []*inboxv1.Action{
				{Name: "approve", Label: "Approve All", Variant: "success"},
				{Name: "reject", Label: "Reject", Variant: "error"},
			},
			ClientCompletable: true,
		},
	}); err != nil {
		return err
	}

	// Payment terms decision (multi-choice)
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Payment terms decision",
		Description: "Non-standard payment terms requested. Select appropriate terms based on credit assessment.",
		Tags:        []string{"type:review", "priority:normal", "team:finance"},
		Actor:       "workflow:contract-review",
		Payload: &inboxv1.ItemSchema{
			Display: []*inboxv1.DisplayField{
				{Label: "Customer", Value: "Acme Corp"},
				{Label: "Credit Score", Value: "B+"},
				{Label: "Requested Terms", Value: "Net 60"},
			},
			Fields: []*inboxv1.FormField{
				{Name: "terms", Type: "select", Label: "Payment Terms", Options: []string{"Net 30 (standard)", "Net 60 (extended)", "Net 90 (enterprise)", "Prepayment required"}, Required: true},
				{Name: "reason", Type: "textarea", Label: "Justification", Placeholder: "Explain the decision...", Required: true},
			},
			Actions: []*inboxv1.Action{
				{Name: "approve", Label: "Set Terms", Variant: "success"},
			},
		},
	}); err != nil {
		return err
	}

	// PEP screening (document checklist)
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "PEP screening review",
		Description: "Customer flagged as PEP during onboarding. Verify provided documents.",
		Tags:        []string{"type:review", "priority:urgent", "team:compliance"},
		Actor:       "workflow:onboarding-001",
		Deadline:    &deadline,
		Payload: &inboxv1.ItemSchema{
			Display: []*inboxv1.DisplayField{
				{Label: "Customer", Value: "CUST-5678"},
				{Label: "Risk Level", Value: "High"},
				{Label: "Screening Source", Value: "World-Check"},
			},
			Fields: []*inboxv1.FormField{
				{Name: "gov_id", Type: "checkbox", Label: "Government-issued ID", Description: "Passport or Emirates ID verified."},
				{Name: "proof_of_address", Type: "checkbox", Label: "Proof of Address", Description: "Utility bill or bank statement within 3 months."},
				{Name: "bank_statement", Type: "checkbox", Label: "Bank Statement", Description: "Recent bank statement showing source of funds."},
				{Name: "company_reg", Type: "checkbox", Label: "Company Registration", Description: "Certificate of incorporation or trade license."},
				{Name: "notes", Type: "textarea", Label: "Review Notes", Placeholder: "Document your findings..."},
			},
			Actions: []*inboxv1.Action{
				{Name: "clear", Label: "Clear — Low Risk", Variant: "success"},
				{Name: "escalate", Label: "Escalate to MLRO", Variant: "warning"},
				{Name: "reject", Label: "Reject — High Risk", Variant: "error"},
			},
		},
	}); err != nil {
		return err
	}

	// Customer screening (simple approve/reject)
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Sanctions screening review",
		Description: "Automated sanctions screening returned a potential match. Manual review required.",
		Tags:        []string{"type:review", "priority:high", "team:compliance"},
		Actor:       "agent:screening-bot",
		Payload: &inboxv1.ItemSchema{
			Display: []*inboxv1.DisplayField{
				{Label: "Customer", Value: "CUST-9012"},
				{Label: "Match Type", Value: "Name similarity (87%)"},
				{Label: "List", Value: "OFAC SDN"},
				{Label: "Matched Entity", Value: "Ahmad Al-Rashid (DOB: 1965-03-12)"},
				{Label: "Customer DOB", Value: "1990-07-22"},
			},
			Fields: []*inboxv1.FormField{
				{Name: "result", Type: "select", Label: "Decision", Options: []string{"cleared", "true_match", "inconclusive"}, Required: true},
				{Name: "reason", Type: "textarea", Label: "Reason", Placeholder: "Explain your decision...", Required: true},
			},
			Actions: []*inboxv1.Action{
				{Name: "clear", Label: "Clear — False Positive", Variant: "success"},
				{Name: "block", Label: "Block — True Match", Variant: "error"},
			},
		},
	}); err != nil {
		return err
	}

	// Generic item (no schema — falls back to JSON view)
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
