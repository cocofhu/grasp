package workspacegit

import "errors"

// Inventory exposes stable capability reasons, never Git stderr or host paths.
// These codes describe existing rejection rules; they do not relax them.
type repositoryError struct {
	reason string
	cause  error
}

func (e *repositoryError) Error() string { return e.reason }
func (e *repositoryError) Unwrap() error { return e.cause }

func unsupportedRepository(reason string) error {
	return &repositoryError{reason: reason, cause: ErrUnsupported}
}

func repositoryReason(err error) string {
	var reason *repositoryError
	if errors.As(err, &reason) {
		return reason.reason
	}
	if errors.Is(err, ErrLimit) {
		return "repository_limit"
	}
	if errors.Is(err, ErrUnsafe) {
		return "repository_unsafe_metadata"
	}
	return "repository_requires_snapshot"
}
