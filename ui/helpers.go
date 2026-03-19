package ui

import (
	"fmt"
	"time"
)

var eventLabels = map[string]string{
	"inbox.v1.ItemCreated":     "Created",
	"inbox.v1.ItemClaimed":     "Claimed",
	"inbox.v1.ItemReleased":    "Released",
	"inbox.v1.ItemResponded":   "Responded",
	"inbox.v1.ItemCompleted":   "Completed",
	"inbox.v1.ItemCancelled":   "Cancelled",
	"inbox.v1.ItemExpired":     "Expired",
	"inbox.v1.CommentAppended": "Comment",
	"inbox.v1.ItemEscalated":   "Escalated",
	"inbox.v1.ItemReassigned":  "Reassigned",
	"inbox.v1.TagsChanged":     "Tags changed",
	"inbox.v1.PayloadUpdated":  "Payload updated",
}

func eventLabel(dataType string) string {
	if label, ok := eventLabels[dataType]; ok {
		return label
	}
	return dataType
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func statusBadgeVariant(status string) string {
	switch status {
	case "open":
		return "badge-info"
	case "claimed":
		return "badge-warning"
	case "completed":
		return "badge-success"
	case "cancelled":
		return "badge-neutral"
	case "expired":
		return "badge-error"
	default:
		return ""
	}
}

// actorDisplayName extracts a human-readable name from an actor string.
// "user:compliance:fatima" -> "fatima"
// "agent:triage-bot" -> "triage-bot"
// "system" -> "system"
func actorDisplayName(actor string) string {
	for i := len(actor) - 1; i >= 0; i-- {
		if actor[i] == ':' {
			return actor[i+1:]
		}
	}
	return actor
}
