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

// filterKey returns the query param key for a filter, stripping the trailing colon.
// e.g. "priority:" → "priority"
func filterKey(tagPrefix string) string {
	if len(tagPrefix) > 0 && tagPrefix[len(tagPrefix)-1] == ':' {
		return tagPrefix[:len(tagPrefix)-1]
	}
	return tagPrefix
}

// buildFilterSignalsJSON builds a JSON string for Datastar data-signals
// with clean filter keys mapped to their active values.
func buildFilterSignalsJSON(filters []FilterConfig, active map[string]string) string {
	buf := []byte("{")
	for i, f := range filters {
		if i > 0 {
			buf = append(buf, ',')
		}
		key := filterKey(f.TagPrefix)
		val := active[f.Label]
		buf = append(buf, '"')
		buf = append(buf, key...)
		buf = append(buf, `":"`...)
		buf = append(buf, val...)
		buf = append(buf, '"')
	}
	buf = append(buf, '}')
	return string(buf)
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
