package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/webworkspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToWebWorkspaceSessionDtoMapsURLFreePageHealth(t *testing.T) {
	session := &webworkspace.Session{
		Id:    "session-1",
		State: webworkspace.AgentStateRunning,
		Page: &webworkspace.AgentPageStatus{
			State:     "RETRYING",
			Error:     "ERR_TUNNEL_CONNECTION_FAILED",
			Attempts:  2,
			UpdatedAt: 1789800000,
		},
		IMEState: "READY",
	}

	result := toWebWorkspaceSessionDto(session)
	require.NotNil(t, result.Page)
	assert.Equal(t, session.Page.State, result.Page.State)
	assert.Equal(t, session.Page.Error, result.Page.Error)
	assert.Equal(t, session.Page.Attempts, result.Page.Attempts)
	assert.Equal(t, session.Page.UpdatedAt, result.Page.UpdatedAt)
	assert.Equal(t, "READY", result.IMEState)
	assert.Nil(t, toWebWorkspaceSessionDto(&webworkspace.Session{}).Page)
	assert.Equal(t, "UNAVAILABLE", toWebWorkspaceSessionDto(&webworkspace.Session{}).IMEState)

	raw, err := common.Marshal(result)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(raw, &payload))
	_, hasPage := payload["page"]
	assert.True(t, hasPage)
	assertNoURLKey(t, payload)
}

func assertNoURLKey(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			assert.False(t, strings.Contains(strings.ToLower(key), "url"), "unexpected URL field %q", key)
			assertNoURLKey(t, child)
		}
	case []any:
		for _, child := range typed {
			assertNoURLKey(t, child)
		}
	}
}
