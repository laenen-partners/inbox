package main

import (
	"context"
	"time"

	"github.com/laenen-partners/identity"
	"github.com/laenen-partners/inbox"
	schemav1 "github.com/laenen-partners/inbox/schema/gen/schema/v1"
	"github.com/laenen-partners/tags"
	"google.golang.org/protobuf/types/known/structpb"
)

func seedData(parentCtx context.Context, ib *inbox.Inbox) error {
	seedID, _ := identity.New("seed", "seed", "seed", identity.PrincipalService, nil)
	ctx := identity.WithContext(parentCtx, seedID)
	// Skip if items already exist
	existing, _ := ib.ListByTags(ctx, []string{tags.Status(inbox.StatusOpen)}, inbox.ListOpts{PageSize: 1})
	if len(existing) > 0 {
		return nil
	}

	deadline := time.Now().Add(48 * time.Hour)

	// --- KYC document upload (file + image fields) ---
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Upload identity documents",
		Description: "Please upload your identity document and a recent proof of address to continue opening your account.",
		Tags:        tags.MustNew("type:input_required", "priority:high", tags.Team("ops"), "assignee:customer:cust-1234"),

		Deadline: &deadline,
		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{
				{Label: "Customer", Value: "CUST-1234"},
				{Label: "Product", Value: "Current Account — AED"},
				{Type: "alert", Variant: "info", Value: "Accepted documents: passport, national ID, or driver's license. Must be valid and not expired."},
			},
			Fields: []*schemav1.FormField{
				{Name: "id_doc", Type: "file", Label: "Identity Document", Description: "Passport, national ID, or driver's license.", Accept: ".pdf,.jpg,.png", MaxSizeMb: 10, Required: true},
				{Name: "selfie", Type: "image", Label: "Selfie with ID", Description: "Take a photo holding your ID next to your face.", MaxSizeMb: 5, Required: true},
				{Name: "proof_address", Type: "file", Label: "Proof of Address", Description: "Utility bill or bank statement within last 3 months.", Accept: ".pdf,.jpg,.png", MaxSizeMb: 10, Required: true},
			},
			Actions: []*schemav1.Action{
				{Name: "submit", Label: "Submit Documents", Variant: "success"},
			},
			ClientCompletable: true,
		},
	}); err != nil {
		return err
	}

	// --- Contract review (document display + signature) ---
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Review and sign service agreement",
		Description: "Please review the service agreement and sign to activate your account.",
		Tags:        tags.MustNew("type:approval", "priority:normal", tags.Team("ops"), "assignee:customer:cust-1234"),

		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{
				{Label: "Customer", Value: "CUST-1234"},
				{Type: "document", Label: "Service Agreement v2.1", Value: "/assets/docs/agreement.pdf"},
				{Type: "alert", Variant: "warning", Value: "Please read the full agreement before signing. This is a legally binding document."},
			},
			Fields: []*schemav1.FormField{
				{Name: "read_agreement", Type: "checkbox", Label: "I have read and understood the service agreement", Required: true},
				{Name: "signature", Type: "signature", Label: "Signature", Description: "Sign in the box below."},
			},
			Actions: []*schemav1.Action{
				{Name: "sign", Label: "Sign Agreement", Variant: "success"},
				{Name: "request_changes", Label: "Request Changes", Variant: "outline"},
			},
			ClientCompletable: true,
		},
	}); err != nil {
		return err
	}

	// --- Email verification (OTP field) ---
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Verify your email address",
		Description: "We sent a 6-digit code to your email. Enter it below to verify your address.",
		Tags:        tags.MustNew("type:action", "priority:high", tags.Team("ops"), "assignee:customer:cust-2000"),

		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{
				{Label: "Email", Value: "j.doe@example.com"},
				{Type: "alert", Variant: "info", Value: "Check your inbox for a message from noreply@neo.app. The code expires in 10 minutes."},
			},
			Fields: []*schemav1.FormField{
				{Name: "code", Type: "otp", Label: "Verification Code", Required: true},
			},
			Actions: []*schemav1.Action{
				{Name: "verify", Label: "Verify Email", Variant: "success"},
			},
			ClientCompletable: true,
		},
	}); err != nil {
		return err
	}

	// --- Insurance claim (image upload + date + number) ---
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Document vehicle damage",
		Description: "Please provide photos and details of the damage for your insurance claim.",
		Tags:        tags.MustNew("type:input_required", "priority:normal", tags.Team("ops"), "assignee:customer:cust-3000"),

		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{
				{Label: "Claim", Value: "CLM-2026-0099", Mono: true},
				{Label: "Policy", Value: "Comprehensive Auto — POL-445566"},
			},
			Fields: []*schemav1.FormField{
				{Name: "photos", Type: "image", Label: "Damage Photos", Description: "Take clear photos of all damaged areas.", Multiple: true, MaxSizeMb: 10, Required: true},
				{Name: "incident_date", Type: "date", Label: "Date of Incident", Required: true},
				{Name: "estimate", Type: "number", Label: "Estimated Repair Cost", Placeholder: "0.00", Required: true},
				{Name: "description", Type: "textarea", Label: "Description", Placeholder: "Describe what happened...", Required: true},
			},
			Actions: []*schemav1.Action{
				{Name: "submit", Label: "Submit Claim", Variant: "success"},
			},
			ClientCompletable: true,
		},
	}); err != nil {
		return err
	}

	// --- Service feedback (rating + textarea) ---
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Rate your onboarding experience",
		Description: "We'd love to hear your feedback on the account opening process.",
		Tags:        tags.MustNew("type:action", "priority:low", tags.Team("ops"), "assignee:customer:cust-4000"),

		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{
				{Type: "alert", Variant: "success", Label: "Account activated", Value: "Your Current Account is now ready to use."},
			},
			Fields: []*schemav1.FormField{
				{Name: "rating", Type: "rating", Label: "Overall Experience"},
				{Name: "feedback", Type: "textarea", Label: "Comments", Placeholder: "What went well? What could we improve?"},
				{Name: "contact_ok", Type: "checkbox", Label: "May we follow up?", Placeholder: "You can contact me about my feedback"},
			},
			Actions: []*schemav1.Action{
				{Name: "submit", Label: "Submit Feedback", Variant: "neutral"},
			},
			ClientCompletable: true,
		},
	}); err != nil {
		return err
	}

	// --- Employment details (phone, email, number fields) ---
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Provide employment details",
		Description: "We need your employment and income information for the account assessment.",
		Tags:        tags.MustNew("type:input_required", "priority:normal", tags.Team("ops"), "assignee:customer:cust-1234"),

		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{
				{Label: "Customer", Value: "CUST-1234"},
			},
			Fields: []*schemav1.FormField{
				{Name: "employer", Type: "text", Label: "Employer Name", Required: true},
				{Name: "job_title", Type: "text", Label: "Job Title", Required: true},
				{Name: "work_phone", Type: "phone", Label: "Work Phone"},
				{Name: "work_email", Type: "email", Label: "Work Email"},
				{Name: "annual_income", Type: "number", Label: "Annual Income (AED)", Required: true},
				{Name: "employment_date", Type: "date", Label: "Employment Start Date"},
			},
			Actions: []*schemav1.Action{
				{Name: "submit", Label: "Submit Details", Variant: "success"},
			},
			ClientCompletable: true,
		},
	}); err != nil {
		return err
	}

	// --- Operator: sanctions screening (no customer access) ---
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Sanctions screening review",
		Description: "Automated screening returned a potential match. Manual review required.",
		Tags:        tags.MustNew("type:review", "priority:high", tags.Team("compliance")),

		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{
				{Label: "Customer", Value: "CUST-9012"},
				{Label: "Match Type", Value: "Name similarity (87%)"},
				{Label: "List", Value: "OFAC SDN"},
				{Label: "Matched Entity", Value: "Ahmad Al-Rashid (DOB: 1965-03-12)"},
				{Label: "Customer DOB", Value: "1990-07-22"},
				{Type: "alert", Variant: "warning", Value: "High confidence name match. Compare dates of birth and nationality before clearing."},
			},
			Fields: []*schemav1.FormField{
				{Name: "result", Type: "select", Label: "Decision", Options: []string{"cleared", "true_match", "inconclusive"}, Required: true},
				{Name: "reason", Type: "textarea", Label: "Reason", Placeholder: "Explain your decision...", Required: true},
			},
			Actions: []*schemav1.Action{
				{Name: "clear", Label: "Clear — False Positive", Variant: "success"},
				{Name: "block", Label: "Block — True Match", Variant: "error"},
			},
		},
	}); err != nil {
		return err
	}

	// --- Operator: PEP screening with document checklist ---
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "PEP screening review",
		Description: "Customer flagged as PEP during onboarding. Verify provided documents.",
		Tags:        tags.MustNew("type:review", "priority:urgent", tags.Team("compliance")),

		Deadline: &deadline,
		Payload: &schemav1.ItemSchema{
			Display: []*schemav1.DisplayField{
				{Label: "Customer", Value: "CUST-5678"},
				{Label: "Risk Level", Value: "High"},
				{Label: "Screening Source", Value: "World-Check"},
				{Type: "alert", Variant: "error", Value: "Enhanced due diligence required. At least 2 verification documents must be confirmed."},
			},
			Fields: []*schemav1.FormField{
				{Name: "docs_verified", Type: "multiselect", Label: "Verified Documents", Options: []string{"Government ID", "Proof of Address", "Bank Statement", "Company Registration", "Tax ID"}},
				{Name: "notes", Type: "textarea", Label: "Review Notes", Placeholder: "Document your findings..."},
			},
			Actions: []*schemav1.Action{
				{Name: "clear", Label: "Clear — Low Risk", Variant: "success"},
				{Name: "escalate", Label: "Escalate to MLRO", Variant: "warning"},
				{Name: "reject", Label: "Reject — High Risk", Variant: "error"},
			},
		},
	}); err != nil {
		return err
	}

	// --- Generic item (no schema — JSON fallback) ---
	genericPayload, _ := structpb.NewStruct(map[string]interface{}{
		"source": "system",
		"note":   "Travel expenses submitted for Q1 conference",
		"amount": 2450.00,
	})
	if _, err := ib.Create(ctx, inbox.Meta{
		Title:       "Expense report approval",
		Description: "Travel expenses submitted for Q1 conference.",
		Tags:        tags.MustNew("type:approval", "priority:low", tags.Team("finance")),

		Payload: genericPayload,
	}); err != nil {
		return err
	}

	return nil
}
