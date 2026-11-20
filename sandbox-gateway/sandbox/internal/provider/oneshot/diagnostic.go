package oneshot

import "backend/internal/provider"

// emitError sends CLI diagnostics before the turn boundary, credentials hidden.
func (e *engine) emitError(err error) {
	e.emit(map[string]any{"op": "raw", "type": "error_text", "text": provider.RedactDiagnostic(err.Error(), e.env)})
}

// The bridge also logs and broadcasts Prompt's returned error. Keep that path
// redacted too, while preserving cancellation and native exit error matching.
type safeDiagnosticError struct {
	cause error
	text  string
}

func (e *safeDiagnosticError) Error() string { return e.text }
func (e *safeDiagnosticError) Unwrap() error { return e.cause }

func (e *engine) safeError(err error) error {
	text := provider.RedactDiagnostic(err.Error(), e.env)
	if text == err.Error() {
		return err
	}
	return &safeDiagnosticError{cause: err, text: text}
}
