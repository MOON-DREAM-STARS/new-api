package controller

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipRelayErrorBodyAfterResponsesTerminalEvent(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.False(t, shouldSkipRelayErrorBody(c))
	require.False(t, shouldSkipRelayErrorBody(nil))

	c.Set(relaycommon.ContextKeyResponsesTerminalEventSent, true)
	require.True(t, shouldSkipRelayErrorBody(c))
}
