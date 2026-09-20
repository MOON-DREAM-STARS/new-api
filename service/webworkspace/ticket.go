package webworkspace

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// StreamTicketTTL is deliberately short: the ticket is consumed by a single
// WebSocket upgrade and is invalid afterwards.
const StreamTicketTTL = 30 * time.Second

// ErrStreamTicketInvalid covers unknown, expired, replayed and mismatched
// tickets with one non-informative error.
var ErrStreamTicketInvalid = errors.New("web workspace stream ticket invalid")

// StreamTicket binds one stream attach to a user, workspace and session.
type StreamTicket struct {
	Token       string
	UserId      int
	WorkspaceId int
	SessionId   string
	ExpiresAt   int64
}

type ticketStore struct {
	mutex   sync.Mutex
	entries map[string]StreamTicket
}

var tickets = ticketStore{entries: make(map[string]StreamTicket)}

func (s *ticketStore) pruneLocked(now int64) {
	for token, entry := range s.entries {
		if entry.ExpiresAt < now {
			delete(s.entries, token)
		}
	}
}

// IssueStreamTicket creates a single-use ticket for one session.
func IssueStreamTicket(session *Session) (string, int64, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", 0, err
	}
	expiresAt := time.Now().Add(StreamTicketTTL).Unix()
	tickets.mutex.Lock()
	tickets.pruneLocked(time.Now().Unix())
	tickets.entries[token] = StreamTicket{
		Token:       token,
		UserId:      session.UserId,
		WorkspaceId: session.WorkspaceId,
		SessionId:   session.Id,
		ExpiresAt:   expiresAt,
	}
	tickets.mutex.Unlock()
	return token, expiresAt, nil
}

// validateStreamTicketLocked validates one ticket without consuming it.
func validateStreamTicketLocked(token string, sessionId string, prune bool) (StreamTicket, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return StreamTicket{}, ErrStreamTicketInvalid
	}
	now := time.Now().Unix()
	if prune {
		tickets.pruneLocked(now)
	}
	entry, ok := tickets.entries[token]
	if !ok || entry.ExpiresAt < now || entry.SessionId != sessionId {
		return StreamTicket{}, ErrStreamTicketInvalid
	}
	return entry, nil
}

// ValidateStreamTicket validates a ticket without consuming it. It is used for
// the KasmVNC document and its relative assets, which cannot carry an
// Authorization header. The ticket remains short-lived and is consumed when the
// Kasm websocket upgrade is authorized.
func ValidateStreamTicket(token string, sessionId string) (StreamTicket, error) {
	tickets.mutex.Lock()
	defer tickets.mutex.Unlock()
	return validateStreamTicketLocked(token, sessionId, true)
}

// ConsumeStreamTicket validates and burns a ticket. The ticket is removed even
// when the remaining checks fail, so a replay can never succeed. Callers must
// still resolve the session and confirm the session belongs to the same user.
func ConsumeStreamTicket(token string, sessionId string) (StreamTicket, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return StreamTicket{}, ErrStreamTicketInvalid
	}
	tickets.mutex.Lock()
	defer tickets.mutex.Unlock()
	entry, err := validateStreamTicketLocked(token, sessionId, true)
	if err != nil {
		delete(tickets.entries, token)
		return StreamTicket{}, ErrStreamTicketInvalid
	}
	delete(tickets.entries, token)
	return entry, nil
}
