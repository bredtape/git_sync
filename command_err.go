package git_sync

import (
	"fmt"
)

// CommandError is a custom error struct for capturing detailed command execution errors
type CommandError struct {
	// Error is the underlying error that occurred
	Err error

	// Message is an optional error message
	Message string

	// ExitCode represents the exit status or error code of the command
	ExitCode int

	// StdErr contains the error output from the command execution
	StdErr string
}

// Implement the error interface
func (e *CommandError) Error() string {
	// Provide a comprehensive error message
	return fmt.Sprintf("Command error '%s': %v (Status: %d, StdErr: %s)",
		e.Message,
		e.Err,
		e.ExitCode,
		e.StdErr)
}

// NewCommandError creates a new CommandError instance
func NewCommandError(err error, msg string, status int, stdErr string) *CommandError {
	return &CommandError{
		Err:      err,
		Message:  msg,
		ExitCode: status,
		StdErr:   stdErr,
	}
}

// Unwrap allows for error unwrapping, supporting Go 1.13+ error handling
func (e *CommandError) Unwrap() error {
	return e.Err
}
