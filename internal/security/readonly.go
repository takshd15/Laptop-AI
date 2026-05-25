package security

import "errors"

// ErrReadOnlyViolation is returned when caller attempts a prohibited write operation.
var ErrReadOnlyViolation = errors.New("operation not permitted: assistant is read-only")

// ReadOnlyEnforcer documents and enforces the read-only boundary for the AI assistant.
//
// In v1, safety is structural: the codebase simply has no functions that execute
// shell commands, delete files, edit files, send email, or make AI-initiated network
// calls. This type exists so that future code that wants to add such capabilities
// must pass through an explicit gate — if ReadOnly is true the call is rejected.
type ReadOnlyEnforcer struct {
	// ReadOnly controls whether prohibited operations are blocked.
	// Default true. Set false only in future admin/tool mode with explicit user opt-in.
	ReadOnly bool
}

// DefaultEnforcer is the singleton used by all AI-facing paths.
var DefaultEnforcer = &ReadOnlyEnforcer{ReadOnly: true}

// GuardShellExec rejects any attempt to run a shell command from AI code.
func (e *ReadOnlyEnforcer) GuardShellExec(cmd string) error {
	if e.ReadOnly {
		return ErrReadOnlyViolation
	}
	return nil
}

// GuardFileWrite rejects any attempt to write or delete a file from AI code.
func (e *ReadOnlyEnforcer) GuardFileWrite(path string) error {
	if e.ReadOnly {
		return ErrReadOnlyViolation
	}
	return nil
}

// GuardNetworkCall rejects any AI-initiated outbound network request that is not
// a configured local Ollama call.
func (e *ReadOnlyEnforcer) GuardNetworkCall(url string) error {
	if e.ReadOnly {
		return ErrReadOnlyViolation
	}
	return nil
}

// PermittedActions documents what the read-only assistant IS allowed to do.
// Returned for display in help text or --explain-safety output.
func PermittedActions() []string {
	return []string{
		"Read pre-indexed chunks from the local vector database",
		"Answer questions using retrieved context",
		"Show sources (file paths and chunk positions)",
		"Admit when context is insufficient to answer",
	}
}
