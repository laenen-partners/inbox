package inbox

const (
	StatusOpen    = "open"
	StatusClaimed = "claimed"
	StatusClosed  = "closed"
)

// IsTerminal returns true if the status is terminal.
// Uses != open && != claimed to handle legacy statuses (completed, cancelled, expired).
func IsTerminal(status string) bool {
	return status != StatusOpen && status != StatusClaimed
}
