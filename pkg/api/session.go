package api

import "github.com/ageneralai/ageneral-agents-go/pkg/message"

// SessionStore persists conversation history across process restarts.
// Implementations must be safe for concurrent use.
type SessionStore interface {
	// Load returns prior conversation history for sessionID, or nil if not found.
	Load(sessionID string) ([]message.Message, error)
	// Save persists the full current history for sessionID.
	Save(sessionID string, msgs []message.Message) error
}
