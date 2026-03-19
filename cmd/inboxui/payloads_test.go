package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestStructRenderer_AddressRequest(t *testing.T) {
	// Simulate the exact path: structpb.Struct → anypb.Any → GetValue() → renderer
	payload, err := structpb.NewStruct(map[string]interface{}{
		"_type":   "address_request",
		"message": "Provide business address",
		"street":  "123 Main St",
		"city":    "Amsterdam",
		"zip":     "1012 AB",
		"country": "Netherlands",
	})
	if err != nil {
		t.Fatalf("create struct: %v", err)
	}

	// This is what inbox.Create does internally
	anyPb, err := anypb.New(payload)
	if err != nil {
		t.Fatalf("wrap in Any: %v", err)
	}

	// This is what detail.go passes to the renderer
	data := anyPb.GetValue()
	payloadType := string(proto.MessageName(payload))

	t.Logf("PayloadType: %q", payloadType)
	t.Logf("Data length: %d bytes", len(data))
	t.Logf("Data (hex): %x", data)

	component := structRenderer(payloadType, data)
	if component == nil {
		t.Fatal("structRenderer returned nil — expected address component")
	}

	// Render the component to HTML and check content
	var buf bytes.Buffer
	if err := component.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render component: %v", err)
	}

	html := buf.String()
	t.Logf("Rendered HTML length: %d", len(html))

	for _, want := range []string{"Address Required", "Amsterdam", "1012 AB"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q\nGot: %s", want, html)
		}
	}
}

func TestStructRenderer_ConsentRequest(t *testing.T) {
	payload, err := structpb.NewStruct(map[string]interface{}{
		"_type": "consent_request",
		"items": []interface{}{
			map[string]interface{}{
				"name":        "Data Processing Agreement",
				"description": "GDPR consent",
				"required":    true,
			},
		},
	})
	if err != nil {
		t.Fatalf("create struct: %v", err)
	}

	anyPb, _ := anypb.New(payload)
	data := anyPb.GetValue()

	component := structRenderer("google.protobuf.Struct", data)
	if component == nil {
		t.Fatal("structRenderer returned nil — expected consent component")
	}

	var buf bytes.Buffer
	if err := component.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	html := buf.String()
	for _, want := range []string{"Consent Review", "Data Processing Agreement", "required"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in HTML", want)
		}
	}
}

func TestStructRenderer_MultiChoice(t *testing.T) {
	payload, _ := structpb.NewStruct(map[string]interface{}{
		"_type":          "multi_choice",
		"question":       "Select payment terms",
		"allow_multiple": false,
		"options":        []interface{}{"Net 30", "Net 60"},
	})

	anyPb, _ := anypb.New(payload)
	data := anyPb.GetValue()

	component := structRenderer("google.protobuf.Struct", data)
	if component == nil {
		t.Fatal("structRenderer returned nil — expected multi-choice component")
	}

	var buf bytes.Buffer
	component.Render(context.Background(), &buf)
	html := buf.String()

	for _, want := range []string{"Single Choice", "Net 30", "Net 60", "radio"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in HTML", want)
		}
	}
}

func TestStructRenderer_FallsBackForUnknownType(t *testing.T) {
	payload, _ := structpb.NewStruct(map[string]interface{}{
		"note": "generic data",
	})

	anyPb, _ := anypb.New(payload)
	data := anyPb.GetValue()

	component := structRenderer("google.protobuf.Struct", data)
	if component != nil {
		t.Errorf("expected nil for struct without _type, got component")
	}
}
