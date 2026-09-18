package webworkspace

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const syncTestAgentToken = "web-workspace-sync-test-token"

const (
	syncTestProjectA     = "g-p-11111111111111111111111111111111"
	syncTestProjectB     = "g-p-22222222222222222222222222222222"
	syncTestConversation = "11111111-1111-1111-1111-111111111111"
)

type syncOwnershipPush struct {
	WorkspaceId int
	Request     agentOwnershipRequest
}

type syncPermitCall struct {
	WorkspaceId int
	Request     agentPermitRequest
}

// syncAgentServer is a scripted Browser Agent internal API: it serves the
// runtime, ownership, permit and observation endpoints the control plane uses
// and records every call so tests can assert what was published.
type syncAgentServer struct {
	server *httptest.Server
	mutex  sync.Mutex

	runtimes        map[int]AgentRuntime
	log             []Observation
	offset          int
	ownershipPushes []syncOwnershipPush
	permitCalls     []syncPermitCall
	ackOffsets      []int64
	failPermits     bool
	permitExpires   int64
}

func newSyncAgentServer(t *testing.T) *syncAgentServer {
	t.Helper()
	agent := &syncAgentServer{runtimes: make(map[int]AgentRuntime), permitExpires: time.Now().Unix() + 300}
	agent.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+syncTestAgentToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if r.URL.Path == "/internal/v1/runtimes" && r.Method == http.MethodPost {
			var request agentCreateRuntimeRequest
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			now := time.Now().Unix()
			runtime := AgentRuntime{
				RuntimeId:      "ws-" + strconv.Itoa(request.WorkspaceId),
				WorkspaceId:    request.WorkspaceId,
				State:          AgentStateRunning,
				CreatedAt:      now,
				LastActivityAt: now,
				IdleDeadlineAt: now + 600,
			}
			agent.mutex.Lock()
			agent.runtimes[request.WorkspaceId] = runtime
			agent.mutex.Unlock()
			writeSyncAgentJSON(t, w, runtime)
			return
		}
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/runtimes/"), "/"), "/")
		if len(parts) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		workspaceId, err := strconv.Atoi(parts[0])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		suffix := strings.Join(parts[1:], "/")
		if suffix == "" && r.Method == http.MethodGet {
			agent.mutex.Lock()
			runtime, ok := agent.runtimes[workspaceId]
			agent.mutex.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeSyncAgentJSON(t, w, runtime)
			return
		}
		switch {
		case suffix == "ownership" && r.Method == http.MethodPut:
			var request agentOwnershipRequest
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			agent.ownershipPushes = append(agent.ownershipPushes, syncOwnershipPush{WorkspaceId: workspaceId, Request: request})
			agent.mutex.Unlock()
			writeSyncAgentJSON(t, w, struct {
				Generation    int64 `json:"generation"`
				Projects      int   `json:"projects"`
				Conversations int   `json:"conversations"`
			}{request.Generation, len(request.Projects), len(request.Conversations)})
		case suffix == "permits" && r.Method == http.MethodPost:
			var request agentPermitRequest
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			fail := agent.failPermits
			expiresAt := agent.permitExpires
			agent.permitCalls = append(agent.permitCalls, syncPermitCall{WorkspaceId: workspaceId, Request: request})
			agent.mutex.Unlock()
			if fail {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"success":false,"error":"runtime_not_running"}`))
				return
			}
			writeSyncAgentJSON(t, w, struct {
				PermitId  string `json:"permit_id"`
				ExpiresAt int64  `json:"expires_at"`
			}{request.PermitId, expiresAt})
		case suffix == "observations" && r.Method == http.MethodGet:
			agent.mutex.Lock()
			offset := agent.offset
			if offset > len(agent.log) {
				offset = len(agent.log)
			}
			observations := append([]Observation{}, agent.log[offset:]...)
			nextOffset := len(agent.log)
			agent.mutex.Unlock()
			writeSyncAgentJSON(t, w, struct {
				Observations []Observation `json:"observations"`
				NextOffset   int64         `json:"next_offset"`
			}{observations, int64(nextOffset)})
		case suffix == "observations/ack" && r.Method == http.MethodPost:
			var request agentAckRequest
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			agent.ackOffsets = append(agent.ackOffsets, request.Offset)
			if request.Offset >= 0 && request.Offset <= int64(len(agent.log)) {
				agent.offset = int(request.Offset)
			}
			agent.mutex.Unlock()
			writeSyncAgentJSON(t, w, struct {
				Offset int64 `json:"offset"`
			}{request.Offset})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(agent.server.Close)
	return agent
}

func writeSyncAgentJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	raw, err := common.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func (a *syncAgentServer) setObservations(observations ...Observation) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.log = append([]Observation{}, observations...)
	a.offset = 0
}

func (a *syncAgentServer) setFailPermits(fail bool) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.failPermits = fail
}

func (a *syncAgentServer) pushes() []syncOwnershipPush {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return append([]syncOwnershipPush{}, a.ownershipPushes...)
}

func (a *syncAgentServer) issuedPermits() []syncPermitCall {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return append([]syncPermitCall{}, a.permitCalls...)
}

func (a *syncAgentServer) acks() []int64 {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return append([]int64{}, a.ackOffsets...)
}

func setupWebWorkspaceSyncTest(t *testing.T) (*gorm.DB, *syncAgentServer, *model.User, *model.WebWorkspace) {
	t.Helper()
	settings := system_setting.GetWebWorkspaceSettings()
	previousSettings := *settings
	t.Cleanup(func() { *settings = previousSettings })
	settings.Enabled = true
	settings.MinimumRole = common.RoleCommonUser
	settings.AllowedGroups = []string{}
	settings.AgentBaseURL = ""

	t.Setenv("WEB_WORKSPACE_AGENT_TOKEN", syncTestAgentToken)
	sessions = sessionStore{entries: make(map[string]Session), starts: make(map[int]*sync.Mutex)}
	permits = permitStore{entries: make(map[string]ProjectPermit)}

	db := openWebWorkspaceSQLiteTestDB(t)
	useWebWorkspaceTestDB(t, db)

	agent := newSyncAgentServer(t)
	settings.AgentBaseURL = agent.server.URL

	user := createWebWorkspaceTestUser(t, db, "ws-sync-owner")
	workspace, err := EnsureWorkspace(user.Id, DefaultProvider)
	require.NoError(t, err)
	return db, agent, user, workspace
}

func putWebWorkspaceTestSession(t *testing.T, user *model.User, workspace *model.WebWorkspace) Session {
	t.Helper()
	now := time.Now().Unix()
	session := Session{
		Id:          "session-" + strconv.Itoa(user.Id),
		UserId:      user.Id,
		WorkspaceId: workspace.Id,
		RuntimeId:   "ws-" + strconv.Itoa(workspace.Id),
		State:       AgentStateRunning,
		CreatedAt:   now,
		LastSeenAt:  now,
	}
	sessions.put(session)
	return session
}

// assertAuditContains matches one fragment inside the captured audit trail.
func assertAuditContains(t *testing.T, lines *[]string, fragment string) {
	t.Helper()
	assert.Contains(t, strings.Join(*lines, "\n"), fragment)
}

func captureWebWorkspaceAudit(t *testing.T) *[]string {
	t.Helper()
	previous := webWorkspaceAuditLine
	lines := &[]string{}
	webWorkspaceAuditLine = func(line string) { *lines = append(*lines, line) }
	t.Cleanup(func() { webWorkspaceAuditLine = previous })
	return lines
}

func countWebWorkspaceRows(t *testing.T, db *gorm.DB, value any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(value).Where(query, args...).Count(&count).Error)
	return count
}

func TestWebWorkspaceStartSessionPushesOwnership(t *testing.T) {
	db, agent, user, workspace := setupWebWorkspaceSyncTest(t)
	project := &model.WebProject{WorkspaceId: workspace.Id, Provider: DefaultProvider, ExternalProjectId: syncTestProjectA, Name: "Project A"}
	require.NoError(t, db.Create(project).Error)
	require.NoError(t, db.Create(&model.WebConversation{ProjectId: project.Id, Provider: DefaultProvider, ExternalConversationId: syncTestConversation, Title: "Conversation"}).Error)

	session, err := StartSession(context.Background(), user.Id)
	require.NoError(t, err)
	require.True(t, LiveRuntimeState(session.State))

	pushes := agent.pushes()
	require.Len(t, pushes, 1)
	assert.Equal(t, workspace.Id, pushes[0].WorkspaceId)
	assert.Equal(t, project.UpdatedAt, pushes[0].Request.Generation)
	assert.Equal(t, []string{syncTestProjectA}, pushes[0].Request.Projects)
	assert.Equal(t, []string{syncTestConversation}, pushes[0].Request.Conversations)

	_, err = StartSession(context.Background(), user.Id)
	require.NoError(t, err)
	assert.Len(t, agent.pushes(), 2, "reusing a live runtime must republish ownership")
}

func TestWebWorkspaceBuildOwnershipReportsGenerationAndResources(t *testing.T) {
	db, _, _, workspace := setupWebWorkspaceSyncTest(t)

	other := createWebWorkspaceTestUser(t, db, "ws-sync-other")
	otherWorkspace, err := EnsureWorkspace(other.Id, DefaultProvider)
	require.NoError(t, err)

	projectA := &model.WebProject{WorkspaceId: workspace.Id, Provider: DefaultProvider, ExternalProjectId: syncTestProjectA, Name: "Project A"}
	projectB := &model.WebProject{WorkspaceId: workspace.Id, Provider: DefaultProvider, ExternalProjectId: syncTestProjectB, Name: "Project B"}
	foreignProject := &model.WebProject{WorkspaceId: otherWorkspace.Id, Provider: DefaultProvider, ExternalProjectId: "g-p-33333333333333333333333333333333", Name: "Foreign"}
	require.NoError(t, db.Create(projectA).Error)
	require.NoError(t, db.Create(projectB).Error)
	require.NoError(t, db.Create(foreignProject).Error)
	require.NoError(t, db.Create(&model.WebConversation{ProjectId: projectA.Id, Provider: DefaultProvider, ExternalConversationId: syncTestConversation, Title: "Conversation"}).Error)
	require.NoError(t, db.Create(&model.WebConversation{ProjectId: foreignProject.Id, Provider: DefaultProvider, ExternalConversationId: "22222222-2222-2222-2222-222222222222", Title: "Foreign"}).Error)
	require.NoError(t, db.Exec("UPDATE web_projects SET updated_at = ? WHERE id = ?", 1000, projectA.Id).Error)
	require.NoError(t, db.Exec("UPDATE web_projects SET updated_at = ? WHERE id = ?", 2000, projectB.Id).Error)

	snapshot, err := BuildOwnership(workspace.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2000, snapshot.Generation)
	assert.Equal(t, []string{syncTestProjectA, syncTestProjectB}, snapshot.Projects)
	assert.Equal(t, []string{syncTestConversation}, snapshot.Conversations)
}

func TestWebWorkspaceSyncWorkspaceAppliesObservationsIdempotently(t *testing.T) {
	db, agent, _, workspace := setupWebWorkspaceSyncTest(t)
	agent.setObservations(
		Observation{Event: ObservationProjectCreated, PermitId: "permit-from-agent", ExternalProjectId: syncTestProjectA, Slug: "Sync Project", ObservedAt: 10},
		Observation{Event: ObservationConversationCreated, ExternalProjectId: syncTestProjectA, ExternalConversationId: syncTestConversation, ObservedAt: 11},
	)

	require.NoError(t, SyncWorkspace(context.Background(), workspace.Id))
	assert.EqualValues(t, 1, countWebWorkspaceRows(t, db, &model.WebProject{}, "workspace_id = ?", workspace.Id))
	assert.EqualValues(t, 1, countWebWorkspaceRows(t, db, &model.WebConversation{}, "provider = ?", DefaultProvider))
	var stored model.WebProject
	require.NoError(t, db.Where("workspace_id = ?", workspace.Id).First(&stored).Error)
	assert.Equal(t, "Sync Project", stored.Name)
	assert.Equal(t, []int64{2}, agent.acks())

	pushes := agent.pushes()
	require.Len(t, pushes, 1)
	assert.Equal(t, []string{syncTestProjectA}, pushes[0].Request.Projects)
	assert.Equal(t, []string{syncTestConversation}, pushes[0].Request.Conversations)
	assert.Equal(t, stored.UpdatedAt, pushes[0].Request.Generation)

	// Replaying the same batch must not duplicate rows or republish ownership.
	require.NoError(t, SyncWorkspace(context.Background(), workspace.Id))
	assert.EqualValues(t, 1, countWebWorkspaceRows(t, db, &model.WebProject{}, "workspace_id = ?", workspace.Id))
	assert.EqualValues(t, 1, countWebWorkspaceRows(t, db, &model.WebConversation{}, "provider = ?", DefaultProvider))
	assert.Len(t, agent.pushes(), 1)
	assert.Equal(t, []int64{2, 2}, agent.acks())
}

func TestWebWorkspaceApplyObservationsCascadesProjectNotFound(t *testing.T) {
	db, _, _, workspace := setupWebWorkspaceSyncTest(t)
	other := createWebWorkspaceTestUser(t, db, "ws-sync-other")
	otherWorkspace, err := EnsureWorkspace(other.Id, DefaultProvider)
	require.NoError(t, err)

	project := &model.WebProject{WorkspaceId: workspace.Id, Provider: DefaultProvider, ExternalProjectId: syncTestProjectA, Name: "Project A"}
	foreignProject := &model.WebProject{WorkspaceId: otherWorkspace.Id, Provider: DefaultProvider, ExternalProjectId: syncTestProjectB, Name: "Project B"}
	require.NoError(t, db.Create(project).Error)
	require.NoError(t, db.Create(foreignProject).Error)
	require.NoError(t, db.Create(&model.WebConversation{ProjectId: project.Id, Provider: DefaultProvider, ExternalConversationId: syncTestConversation, Title: "Conversation"}).Error)
	require.NoError(t, db.Create(&model.WebConversation{ProjectId: foreignProject.Id, Provider: DefaultProvider, ExternalConversationId: "22222222-2222-2222-2222-222222222222", Title: "Foreign"}).Error)

	batch := []Observation{{Event: ObservationProjectNotFound, ExternalProjectId: syncTestProjectA, ObservedAt: 20}}
	changed, err := ApplyObservations(workspace.Id, batch)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.EqualValues(t, 0, countWebWorkspaceRows(t, db, &model.WebProject{}, "workspace_id = ?", workspace.Id))
	assert.EqualValues(t, 0, countWebWorkspaceRows(t, db, &model.WebConversation{}, "project_id = ?", project.Id))
	assert.EqualValues(t, 1, countWebWorkspaceRows(t, db, &model.WebProject{}, "workspace_id = ?", otherWorkspace.Id))
	assert.EqualValues(t, 1, countWebWorkspaceRows(t, db, &model.WebConversation{}, "project_id = ?", foreignProject.Id))

	changed, err = ApplyObservations(workspace.Id, batch)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestWebWorkspaceApplyObservationsSkipsOrphanConversation(t *testing.T) {
	db, _, _, workspace := setupWebWorkspaceSyncTest(t)
	lines := captureWebWorkspaceAudit(t)

	changed, err := ApplyObservations(workspace.Id, []Observation{{
		Event:                  ObservationConversationCreated,
		ExternalProjectId:      syncTestProjectA,
		ExternalConversationId: syncTestConversation,
		ObservedAt:             30,
	}})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.EqualValues(t, 0, countWebWorkspaceRows(t, db, &model.WebConversation{}, "provider = ?", DefaultProvider))
	assertAuditContains(t, lines, "event=orphan_conversation_skipped")
}

func TestWebWorkspaceApplyObservationsRejectsMalformedIdentifiers(t *testing.T) {
	db, _, _, workspace := setupWebWorkspaceSyncTest(t)
	lines := captureWebWorkspaceAudit(t)

	changed, err := ApplyObservations(workspace.Id, []Observation{{
		Event:             ObservationProjectCreated,
		ExternalProjectId: "ext-not-a-provider-id",
		Slug:              "Malformed",
		ObservedAt:        40,
	}})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.EqualValues(t, 0, countWebWorkspaceRows(t, db, &model.WebProject{}, "workspace_id = ?", workspace.Id))
	assertAuditContains(t, lines, "reason=invalid_external_project_id")
}

func TestWebWorkspaceApplyObservationsKeepsForeignProjectOwnership(t *testing.T) {
	db, _, _, workspace := setupWebWorkspaceSyncTest(t)
	other := createWebWorkspaceTestUser(t, db, "ws-sync-other")
	otherWorkspace, err := EnsureWorkspace(other.Id, DefaultProvider)
	require.NoError(t, err)
	foreignProject := &model.WebProject{WorkspaceId: otherWorkspace.Id, Provider: DefaultProvider, ExternalProjectId: syncTestProjectA, Name: "Foreign"}
	require.NoError(t, db.Create(foreignProject).Error)
	lines := captureWebWorkspaceAudit(t)

	changed, err := ApplyObservations(workspace.Id, []Observation{{
		Event:             ObservationProjectCreated,
		ExternalProjectId: syncTestProjectA,
		Slug:              "Stolen",
		ObservedAt:        50,
	}})
	require.NoError(t, err)
	assert.False(t, changed)
	var stored model.WebProject
	require.NoError(t, db.First(&stored, foreignProject.Id).Error)
	assert.Equal(t, otherWorkspace.Id, stored.WorkspaceId)
	assert.Equal(t, "Foreign", stored.Name)
	assert.EqualValues(t, 1, countWebWorkspaceRows(t, db, &model.WebProject{}, "provider = ?", DefaultProvider))
	assertAuditContains(t, lines, "reason=project_owned_by_another_workspace")
}

func TestWebWorkspaceIssueProjectPermitRequiresLiveSession(t *testing.T) {
	_, agent, user, workspace := setupWebWorkspaceSyncTest(t)

	_, err := IssueProjectPermit(context.Background(), user.Id, 0)
	assert.ErrorIs(t, err, ErrSessionRequired)

	sessions.put(Session{Id: "stopped-session", UserId: user.Id, WorkspaceId: workspace.Id, State: AgentStateStopped})
	_, err = IssueProjectPermit(context.Background(), user.Id, 0)
	assert.ErrorIs(t, err, ErrSessionRequired)
	assert.Empty(t, agent.issuedPermits())
}

func TestWebWorkspaceIssueProjectPermitEnforcesProjectLimit(t *testing.T) {
	db, agent, user, workspace := setupWebWorkspaceSyncTest(t)
	putWebWorkspaceTestSession(t, user, workspace)
	require.NoError(t, db.Create(&model.WebProject{WorkspaceId: workspace.Id, Provider: DefaultProvider, ExternalProjectId: syncTestProjectA, Name: "Project A"}).Error)

	_, err := IssueProjectPermit(context.Background(), user.Id, 1)
	assert.ErrorIs(t, err, ErrProjectLimitReached)
	assert.Empty(t, agent.issuedPermits())

	permit, err := IssueProjectPermit(context.Background(), user.Id, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, permit.PermitId)
	assert.Equal(t, workspace.Id, permit.WorkspaceId)
	assert.Positive(t, permit.ExpiresAt)
	require.Len(t, agent.issuedPermits(), 1)
	assert.Equal(t, ProjectPermitKind, agent.issuedPermits()[0].Request.Kind)
	assert.Equal(t, projectPermitTTLSeconds, agent.issuedPermits()[0].Request.TtlSeconds)
}

func TestWebWorkspaceIssueProjectPermitAuditsIssuedPermit(t *testing.T) {
	_, agent, user, workspace := setupWebWorkspaceSyncTest(t)
	putWebWorkspaceTestSession(t, user, workspace)
	lines := captureWebWorkspaceAudit(t)

	permit, err := IssueProjectPermit(context.Background(), user.Id, 0)
	require.NoError(t, err)
	require.NotEmpty(t, permit.PermitId)
	assertAuditContains(t, lines, "event=permit_issued")
	issued := agent.issuedPermits()
	require.Len(t, issued, 1)
	assert.Equal(t, permit.PermitId, issued[0].Request.PermitId)

	changed, err := ApplyObservations(workspace.Id, []Observation{{
		Event:             ObservationProjectCreated,
		PermitId:          permit.PermitId,
		ExternalProjectId: syncTestProjectA,
		Slug:              "Project A",
		ObservedAt:        60,
	}})
	require.NoError(t, err)
	assert.True(t, changed)
	assertAuditContains(t, lines, "event=permit_matched")
}

func TestWebWorkspaceApplyObservationsAuditsUnmatchedPermits(t *testing.T) {
	_, _, _, workspace := setupWebWorkspaceSyncTest(t)
	permits.put(ProjectPermit{PermitId: "permit-foreign", WorkspaceId: workspace.Id + 1, ExpiresAt: time.Now().Unix() + 300})
	lines := captureWebWorkspaceAudit(t)

	_, err := ApplyObservations(workspace.Id, []Observation{
		{Event: ObservationProjectCreated, PermitId: "permit-unknown", ExternalProjectId: syncTestProjectA, Slug: "A", ObservedAt: 70},
		{Event: ObservationProjectCreated, PermitId: "permit-foreign", ExternalProjectId: syncTestProjectB, Slug: "B", ObservedAt: 71},
	})
	require.NoError(t, err)
	unmatched := 0
	for _, line := range *lines {
		if strings.Contains(line, "event=permit_unmatched") {
			unmatched++
		}
	}
	assert.Equal(t, 2, unmatched)
	assert.NotContains(t, strings.Join(*lines, "\n"), "event=permit_matched")
}

func TestWebWorkspaceIssueProjectPermitFailsClosedWhenAgentRejects(t *testing.T) {
	_, agent, user, workspace := setupWebWorkspaceSyncTest(t)
	putWebWorkspaceTestSession(t, user, workspace)
	agent.setFailPermits(true)

	_, err := IssueProjectPermit(context.Background(), user.Id, 0)
	assert.ErrorIs(t, err, ErrAgentRejected)

	permits.mutex.RLock()
	defer permits.mutex.RUnlock()
	assert.Empty(t, permits.entries)
}

func TestWebWorkspaceSyncWorkspaceFailsClosedWhenAgentUnreachable(t *testing.T) {
	db, agent, _, workspace := setupWebWorkspaceSyncTest(t)
	agent.setObservations(Observation{Event: ObservationProjectCreated, ExternalProjectId: syncTestProjectA, Slug: "A", ObservedAt: 80})

	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := unreachable.URL
	unreachable.Close()
	system_setting.GetWebWorkspaceSettings().AgentBaseURL = unreachableURL

	err := SyncWorkspace(context.Background(), workspace.Id)
	assert.ErrorIs(t, err, ErrAgentUnavailable)
	assert.EqualValues(t, 0, countWebWorkspaceRows(t, db, &model.WebProject{}, "workspace_id = ?", workspace.Id))
	assert.Empty(t, agent.acks())
	assert.Empty(t, agent.pushes())
}

func TestWebWorkspaceSyncWorkspaceDoesNotAcknowledgeFailedApply(t *testing.T) {
	db, agent, _, workspace := setupWebWorkspaceSyncTest(t)
	agent.setObservations(Observation{Event: ObservationProjectCreated, ExternalProjectId: syncTestProjectA, Slug: "A", ObservedAt: 90})

	// A control-plane store that cannot be written must not acknowledge the
	// pulled batch: the next sync has to retry the same lines.
	broken, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = broken

	err = SyncWorkspace(context.Background(), workspace.Id)
	require.Error(t, err)
	assert.Empty(t, agent.acks())
	assert.Empty(t, agent.pushes())

	model.DB = db
	assert.EqualValues(t, 0, countWebWorkspaceRows(t, db, &model.WebProject{}, "workspace_id = ?", workspace.Id))
}

func TestWebWorkspaceSyncWorkspaceFailsClosedWithoutAgentConfiguration(t *testing.T) {
	_, _, _, workspace := setupWebWorkspaceSyncTest(t)
	system_setting.GetWebWorkspaceSettings().AgentBaseURL = ""

	err := SyncWorkspace(context.Background(), workspace.Id)
	assert.ErrorIs(t, err, ErrAgentUnavailable)
}

// runWebWorkspaceSyncChecks exercises the database-facing part of the Phase 4
// control plane against one real database engine.
func runWebWorkspaceSyncChecks(t *testing.T, db *gorm.DB) {
	t.Helper()

	userA := createWebWorkspaceTestUser(t, db, "ws-owner-a")
	userB := createWebWorkspaceTestUser(t, db, "ws-owner-b")
	workspaceA, err := EnsureWorkspace(userA.Id, DefaultProvider)
	require.NoError(t, err)
	workspaceB, err := EnsureWorkspace(userB.Id, DefaultProvider)
	require.NoError(t, err)

	batch := []Observation{
		{Event: ObservationProjectCreated, PermitId: "permit-database", ExternalProjectId: syncTestProjectA, Slug: "Database Project", ObservedAt: 1},
		{Event: ObservationConversationCreated, ExternalProjectId: syncTestProjectA, ExternalConversationId: syncTestConversation, ObservedAt: 2},
	}
	changed, err := ApplyObservations(workspaceA.Id, batch)
	require.NoError(t, err)
	assert.True(t, changed)

	var projectA model.WebProject
	require.NoError(t, db.Where("workspace_id = ?", workspaceA.Id).First(&projectA).Error)
	assert.Equal(t, "Database Project", projectA.Name)
	var conversationCount int64
	require.NoError(t, db.Model(&model.WebConversation{}).Where("project_id = ?", projectA.Id).Count(&conversationCount).Error)
	assert.EqualValues(t, 1, conversationCount)

	// Replaying the applied batch must stay idempotent on every engine.
	changed, err = ApplyObservations(workspaceA.Id, batch)
	require.NoError(t, err)
	assert.False(t, changed)
	var projectCount int64
	require.NoError(t, db.Model(&model.WebProject{}).Where("workspace_id = ?", workspaceA.Id).Count(&projectCount).Error)
	assert.EqualValues(t, 1, projectCount)

	// Renames only touch the local row of their own workspace.
	changed, err = ApplyObservations(workspaceA.Id, []Observation{
		{Event: ObservationProjectRenamed, ExternalProjectId: syncTestProjectA, Slug: "Renamed Project", ObservedAt: 3},
	})
	require.NoError(t, err)
	assert.True(t, changed)
	require.NoError(t, db.First(&projectA, projectA.Id).Error)
	assert.Equal(t, "Renamed Project", projectA.Name)

	// Generation is the newest project row timestamp of the workspace.
	second := &model.WebProject{WorkspaceId: workspaceA.Id, Provider: DefaultProvider, ExternalProjectId: syncTestProjectB, Name: "Second"}
	require.NoError(t, db.Create(second).Error)
	require.NoError(t, db.Exec("UPDATE web_projects SET updated_at = ? WHERE id = ?", int64(1000), projectA.Id).Error)
	require.NoError(t, db.Exec("UPDATE web_projects SET updated_at = ? WHERE id = ?", int64(2000), second.Id).Error)
	snapshot, err := BuildOwnership(workspaceA.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2000, snapshot.Generation)
	assert.Equal(t, []string{syncTestProjectA, syncTestProjectB}, snapshot.Projects)
	assert.Equal(t, []string{syncTestConversation}, snapshot.Conversations)

	// A provider id already registered elsewhere is never moved.
	foreign := &model.WebProject{WorkspaceId: workspaceB.Id, Provider: DefaultProvider, ExternalProjectId: "g-p-44444444444444444444444444444444", Name: "Foreign"}
	require.NoError(t, db.Create(foreign).Error)
	changed, err = ApplyObservations(workspaceA.Id, []Observation{
		{Event: ObservationProjectCreated, ExternalProjectId: foreign.ExternalProjectId, Slug: "Stolen", ObservedAt: 4},
	})
	require.NoError(t, err)
	assert.False(t, changed)
	var storedForeign model.WebProject
	require.NoError(t, db.First(&storedForeign, foreign.Id).Error)
	assert.Equal(t, workspaceB.Id, storedForeign.WorkspaceId)
	assert.Equal(t, "Foreign", storedForeign.Name)

	// project_not_found cascades through the project and its conversations.
	changed, err = ApplyObservations(workspaceA.Id, []Observation{
		{Event: ObservationProjectNotFound, ExternalProjectId: syncTestProjectA, ObservedAt: 5},
	})
	require.NoError(t, err)
	assert.True(t, changed)
	require.NoError(t, db.Model(&model.WebProject{}).Where("id = ?", projectA.Id).Count(&projectCount).Error)
	assert.EqualValues(t, 0, projectCount)
	require.NoError(t, db.Model(&model.WebConversation{}).Where("project_id = ?", projectA.Id).Count(&conversationCount).Error)
	assert.EqualValues(t, 0, conversationCount)
	var foreignProjectCount int64
	require.NoError(t, db.Model(&model.WebProject{}).Where("workspace_id = ?", workspaceB.Id).Count(&foreignProjectCount).Error)
	assert.EqualValues(t, 1, foreignProjectCount)

	changed, err = ApplyObservations(workspaceA.Id, []Observation{
		{Event: ObservationProjectNotFound, ExternalProjectId: syncTestProjectA, ObservedAt: 5},
	})
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestWebWorkspaceSyncDatabaseSQLite(t *testing.T) {
	db := openWebWorkspaceSQLiteTestDB(t)
	useWebWorkspaceTestDB(t, db)
	runWebWorkspaceSyncChecks(t, db)
}

func TestWebWorkspaceSyncDatabaseMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	prepareWebWorkspaceExternalTestDB(t, db)
	useWebWorkspaceTestDB(t, db)
	runWebWorkspaceSyncChecks(t, db)
}

func TestWebWorkspaceSyncDatabasePostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	prepareWebWorkspaceExternalTestDB(t, db)
	useWebWorkspaceTestDB(t, db)
	runWebWorkspaceSyncChecks(t, db)
}
