package main

import (
	"github.com/a-h/templ"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// structRenderer dispatches to a typed templ component based on the _type field
// in a google.protobuf.Struct payload. Falls back to nil (JSON view) for unknown types.
func structRenderer(_ string, data []byte) templ.Component {
	var s structpb.Struct
	if err := proto.Unmarshal(data, &s); err != nil {
		return nil
	}

	fields := s.GetFields()
	typeField := fields["_type"]
	if typeField == nil {
		return nil
	}
	kind := typeField.GetStringValue()

	switch kind {
	case "address_request":
		return addressPayload(fields)
	case "consent_request":
		return consentPayload(fields)
	case "multi_choice":
		return multiChoicePayload(fields)
	default:
		return nil // fall back to JSON view
	}
}
