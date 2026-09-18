package webworkspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebWorkspaceStreamTicketIsSingleUse(t *testing.T) {
	tickets = ticketStore{entries: make(map[string]StreamTicket)}
	session := &Session{Id: "session-1", UserId: 7, WorkspaceId: 11, State: AgentStateRunning}

	token, expiresAt, err := IssueStreamTicket(session)
	require.NoError(t, err)
	assert.Greater(t, expiresAt, time.Now().Unix())

	entry, err := ConsumeStreamTicket(token, session.Id)
	require.NoError(t, err)
	assert.Equal(t, session.UserId, entry.UserId)
	assert.Equal(t, session.WorkspaceId, entry.WorkspaceId)

	_, err = ConsumeStreamTicket(token, session.Id)
	assert.ErrorIs(t, err, ErrStreamTicketInvalid)
}

func TestWebWorkspaceStreamTicketRejectsMismatchAndExpiry(t *testing.T) {
	tickets = ticketStore{entries: make(map[string]StreamTicket)}
	session := &Session{Id: "session-2", UserId: 8, WorkspaceId: 12, State: AgentStateRunning}

	token, _, err := IssueStreamTicket(session)
	require.NoError(t, err)
	_, err = ConsumeStreamTicket(token, "other-session")
	assert.ErrorIs(t, err, ErrStreamTicketInvalid)

	token, _, err = IssueStreamTicket(session)
	require.NoError(t, err)
	tickets.mutex.Lock()
	expired := tickets.entries[token]
	expired.ExpiresAt = time.Now().Add(-time.Second).Unix()
	tickets.entries[token] = expired
	tickets.mutex.Unlock()
	_, err = ConsumeStreamTicket(token, session.Id)
	assert.ErrorIs(t, err, ErrStreamTicketInvalid)

	_, err = ConsumeStreamTicket("", session.Id)
	assert.ErrorIs(t, err, ErrStreamTicketInvalid)
}
