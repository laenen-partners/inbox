package inbox

// Item statuses.
const (
	StatusOpen      = "open"
	StatusClaimed   = "claimed"
	StatusCompleted = "completed"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"
)

// IsTerminal reports whether the status is a terminal state.
func IsTerminal(status string) bool {
	switch status {
	case StatusCompleted, StatusExpired, StatusCancelled:
		return true
	}
	return false
}
