package inbox

import "errors"

var (
	ErrNotFound          = errors.New("inbox: item not found")
	ErrInvalidTransition = errors.New("inbox: invalid status transition")
	ErrTerminalStatus    = errors.New("inbox: item is in terminal status")
)
