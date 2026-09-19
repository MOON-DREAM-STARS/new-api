package webworkspace

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestWebWorkspaceAgentEndToEnd exercises the real Browser Agent: it starts a
// runtime container, opens the display stream through the control plane and
// checks that the VNC banner arrives. It only runs when the acceptance harness
// provides the agent endpoint and service token, so normal unit runs skip it.
func TestWebWorkspaceAgentEndToEnd(t *testing.T) {
	agentURL := strings.TrimSpace(os.Getenv("WEB_WORKSPACE_E2E_AGENT_URL"))
	token := strings.TrimSpace(os.Getenv("WEB_WORKSPACE_E2E_AGENT_TOKEN"))
	if agentURL == "" || token == "" {
		t.Skip("WEB_WORKSPACE_E2E_AGENT_URL / WEB_WORKSPACE_E2E_AGENT_TOKEN are not configured")
	}
	t.Setenv("WEB_WORKSPACE_AGENT_TOKEN", token)

	settings := system_setting.GetWebWorkspaceSettings()
	previousSettings := *settings
	t.Cleanup(func() { *settings = previousSettings })
	settings.Enabled = true
	settings.MinimumRole = common.RoleCommonUser
	settings.AllowedGroups = []string{}
	settings.AgentBaseURL = agentURL

	sessions = sessionStore{entries: make(map[string]Session), starts: make(map[int]*sync.Mutex)}
	tickets = ticketStore{entries: make(map[string]StreamTicket)}

	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.WebWorkspace{}, &model.WebProject{}, &model.WebConversation{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	user := &model.User{
		Username: "ws-e2e-user", AffCode: "aff-ws-e2e-user", Password: "unused",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(user).Error)

	ctx := context.Background()
	session, err := StartSession(ctx, user.Id, 0, 0)
	require.NoError(t, err, "StartSession against the real agent failed")
	require.Truef(t, LiveRuntimeState(session.State), "unexpected runtime state %s", session.State)
	t.Cleanup(func() { _ = StopSession(context.Background(), user.Id, session.Id) })

	conn, response, err := DialAgentStream(ctx, session.WorkspaceId)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	require.NoError(t, err, "agent stream dial failed")
	defer conn.Close()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(20*time.Second)))
	messageType, payload, err := conn.ReadMessage()
	require.NoError(t, err, "reading the display banner failed")

	head := payload
	if len(head) > 20 {
		head = head[:20]
	}
	assert.Equal(t, 2, messageType, "display stream must carry binary frames")
	assert.Truef(t, strings.HasPrefix(string(payload), "RFB "), "expected an RFB banner, got %q", string(head))

	updated, err := GetSession(user.Id, session.Id)
	require.NoError(t, err)
	assert.True(t, LiveRuntimeState(updated.State))
}
