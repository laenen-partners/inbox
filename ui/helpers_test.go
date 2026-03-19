package ui

import (
	"testing"
	"time"
)

func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{2 * 24 * time.Hour, "2d ago"},
	}
	for _, tt := range tests {
		got := formatAge(time.Now().Add(-tt.d))
		if got != tt.want {
			t.Errorf("formatAge(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestEventLabel(t *testing.T) {
	tests := []struct {
		dataType string
		want     string
	}{
		{"inbox.v1.ItemCreated", "Created"},
		{"inbox.v1.ItemClaimed", "Claimed"},
		{"inbox.v1.ItemReleased", "Released"},
		{"inbox.v1.ItemResponded", "Responded"},
		{"inbox.v1.ItemCompleted", "Completed"},
		{"inbox.v1.ItemCancelled", "Cancelled"},
		{"inbox.v1.ItemExpired", "Expired"},
		{"inbox.v1.CommentAppended", "Comment"},
		{"inbox.v1.ItemEscalated", "Escalated"},
		{"inbox.v1.ItemReassigned", "Reassigned"},
		{"inbox.v1.TagsChanged", "Tags changed"},
		{"inbox.v1.PayloadUpdated", "Payload updated"},
		{"custom.v1.SomethingElse", "custom.v1.SomethingElse"},
	}
	for _, tt := range tests {
		got := eventLabel(tt.dataType)
		if got != tt.want {
			t.Errorf("eventLabel(%q) = %q, want %q", tt.dataType, got, tt.want)
		}
	}
}

func TestStatusVariant(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"open", "badge-info"},
		{"claimed", "badge-warning"},
		{"completed", "badge-success"},
		{"cancelled", "badge-neutral"},
		{"expired", "badge-error"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		got := statusBadgeVariant(tt.status)
		if got != tt.want {
			t.Errorf("statusBadgeVariant(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}
