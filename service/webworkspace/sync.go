package webworkspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Observation event names emitted by the Browser Agent guard (contract §3/§4).
const (
	ObservationProjectCreated      = "project_created"
	ObservationProjectRenamed      = "project_renamed"
	ObservationConversationCreated = "conversation_created"
	ObservationProjectNotFound     = "project_not_found"
)

// ProjectPermitKind is the only creation permit kind in Phase 4.
const ProjectPermitKind = "project_create"

// projectPermitTTLSeconds is the short permit lifetime frozen by contract §5.
const projectPermitTTLSeconds = 300

// projectDisplayNameMaxRunes is the frozen limit of the operator-facing project
// name. The guard types it into the real provider UI, so the control plane and
// the agent enforce the same bound.
const projectDisplayNameMaxRunes = 64

// provider-side identifier shapes of the Phase 4 ChatGPT URL contract.
var (
	externalProjectIdPattern      = regexp.MustCompile(`^g-p-[0-9a-f]{32}$`)
	externalConversationIdPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

var (
	// ErrSessionRequired means the caller has no live browser session, so no
	// creation permit can be bound to a runtime.
	ErrSessionRequired = errors.New("web workspace session required")
	// ErrProjectLimitReached means the workspace already holds the configured
	// maximum number of registered projects.
	ErrProjectLimitReached = errors.New("web workspace project limit reached")
	// ErrInvalidProjectName means the requested project name is empty or longer
	// than the frozen limit.
	ErrInvalidProjectName = errors.New("web workspace project name is invalid")
	// ErrProjectCreationInProgress means the guard is still running a project
	// creation for this workspace, so a second permit would start over.
	ErrProjectCreationInProgress = errors.New("web workspace project creation in progress")
	// ErrCapacityReached means the agent's global active-runtime cap is full.
	ErrCapacityReached = errors.New("web workspace runtime capacity reached")
)

// Observation is one guard observation line. Unknown events are ignored so a
// newer agent contract cannot break the control plane.
type Observation struct {
	Event                  string `json:"event"`
	PermitId               string `json:"permit_id"`
	ExternalProjectId      string `json:"external_project_id"`
	ExternalConversationId string `json:"external_conversation_id"`
	Slug                   string `json:"slug"`
	DisplayName            string `json:"display_name"`
	ObservedAt             int64  `json:"observed_at"`
}

// ProjectPermit is the control plane record of one issued creation permit. It
// backs the permit_matched / permit_unmatched audit and is never persisted.
type ProjectPermit struct {
	PermitId    string
	WorkspaceId int
	ExpiresAt   int64
}

type permitStore struct {
	mutex   sync.RWMutex
	entries map[string]ProjectPermit
}

var permits = permitStore{entries: make(map[string]ProjectPermit)}

func (s *permitStore) put(permit ProjectPermit) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]ProjectPermit)
	}
	s.entries[permit.PermitId] = permit
}

// matched reports whether this process issued the permit for that workspace.
// Expiry is deliberately not re-checked: a permit consumed just before its
// deadline is still legitimately ours when the observation arrives later.
func (s *permitStore) matched(workspaceId int, permitId string) bool {
	if permitId == "" {
		return false
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	permit, ok := s.entries[permitId]
	return ok && permit.WorkspaceId == workspaceId
}

// webWorkspaceAuditLine receives the formatted audit line; tests replace it to
// capture the audit trail instead of writing to the process log.
var webWorkspaceAuditLine func(line string)

// webWorkspaceAudit writes one audit event. Fields are rendered as key=value
// pairs; provider-side ids, paths, queries and tokens never appear here.
func webWorkspaceAudit(event string, fields ...string) {
	line := "web_workspace event=" + event
	for _, field := range fields {
		line += " " + field
	}
	if webWorkspaceAuditLine != nil {
		webWorkspaceAuditLine(line)
		return
	}
	common.SysLog(line)
}

func auditObservationSkipped(workspaceId int, reason string) {
	webWorkspaceAudit("observations_applied", fmt.Sprintf("workspace_id=%d", workspaceId), "result=skipped", "reason="+reason)
}

// normalizeProjectDisplayName trims the operator-facing project name and
// enforces the frozen length limit. The guard types the name into the real
// provider UI, so an empty or oversized name is refused instead of being
// silently rewritten.
func normalizeProjectDisplayName(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	count := utf8.RuneCountInString(trimmed)
	if count < 1 || count > projectDisplayNameMaxRunes {
		return "", false
	}
	return trimmed, true
}

// IssueProjectPermit issues one short-lived creation permit for the caller's
// live session. It never creates a local project row: the guard reports
// project_created only after the provider really opened the project. The
// display name travels with the permit so the guard can create the project
// without the operator touching the cropped provider sidebar.
func IssueProjectPermit(ctx context.Context, userId int, maxProjects int, displayName string) (*ProjectPermit, error) {
	if userId <= 0 {
		return nil, ErrSessionRequired
	}
	name, ok := normalizeProjectDisplayName(displayName)
	if !ok {
		return nil, ErrInvalidProjectName
	}
	session, ok := sessions.currentForUser(userId)
	if !ok || !LiveRuntimeState(session.State) {
		return nil, ErrSessionRequired
	}
	if maxProjects > 0 {
		var registered int64
		if err := model.DB.Model(&model.WebProject{}).Where("workspace_id = ?", session.WorkspaceId).Count(&registered).Error; err != nil {
			return nil, err
		}
		if registered >= int64(maxProjects) {
			return nil, ErrProjectLimitReached
		}
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	// A creation the guard is still running must not be replaced by a second
	// permit. The check is best effort: the guard keeps the final decision and
	// a runtime the agent no longer knows about is reported by the permit call
	// itself.
	if runtime, err := client.GetRuntime(ctx, session.WorkspaceId); err == nil {
		if creation := runtime.ProjectCreation.normalized(); creation != nil && creation.State == ProjectCreationStateRunning {
			return nil, ErrProjectCreationInProgress
		}
	}
	permitId, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	expiresAt, err := client.IssueProjectPermit(ctx, session.WorkspaceId, permitId, projectPermitTTLSeconds, name)
	if err != nil {
		return nil, err
	}
	permit := ProjectPermit{PermitId: permitId, WorkspaceId: session.WorkspaceId, ExpiresAt: expiresAt}
	permits.put(permit)
	webWorkspaceAudit("permit_issued", fmt.Sprintf("workspace_id=%d", session.WorkspaceId))
	return &permit, nil
}

// OwnershipSnapshot is the generation-stamped ownership document pushed to the
// guard. It carries provider-side identifiers only.
type OwnershipSnapshot struct {
	Generation    int64
	Projects      []string
	Conversations []string
}

// BuildOwnership assembles the ownership document of one workspace. The
// generation is the newest project row timestamp of the workspace, zero when
// no project row exists (contract §5).
func BuildOwnership(workspaceId int) (*OwnershipSnapshot, error) {
	snapshot := &OwnershipSnapshot{Projects: []string{}, Conversations: []string{}}
	if workspaceId <= 0 {
		return snapshot, nil
	}
	projects := make([]model.WebProject, 0)
	if err := model.DB.Where("workspace_id = ?", workspaceId).Order("id ASC").Find(&projects).Error; err != nil {
		return nil, err
	}
	projectIds := make([]int, 0, len(projects))
	for i := range projects {
		snapshot.Projects = append(snapshot.Projects, projects[i].ExternalProjectId)
		projectIds = append(projectIds, projects[i].Id)
		if projects[i].UpdatedAt > snapshot.Generation {
			snapshot.Generation = projects[i].UpdatedAt
		}
	}
	if len(projectIds) == 0 {
		return snapshot, nil
	}
	conversations := make([]model.WebConversation, 0)
	if err := model.DB.Where("project_id IN ?", projectIds).Order("id ASC").Find(&conversations).Error; err != nil {
		return nil, err
	}
	for i := range conversations {
		snapshot.Conversations = append(snapshot.Conversations, conversations[i].ExternalConversationId)
	}
	return snapshot, nil
}

// PushOwnership republishes the workspace ownership document to the agent so
// the guard can rebuild its allowlist.
func PushOwnership(ctx context.Context, workspaceId int) error {
	client, err := newAgentClient()
	if err != nil {
		return err
	}
	return pushOwnershipWithClient(ctx, client, workspaceId)
}

func pushOwnershipWithClient(ctx context.Context, client *AgentClient, workspaceId int) error {
	snapshot, err := BuildOwnership(workspaceId)
	if err != nil {
		return err
	}
	if err := client.PutOwnership(ctx, workspaceId, snapshot.Generation, snapshot.Projects, snapshot.Conversations); err != nil {
		return err
	}
	webWorkspaceAudit(
		"ownership_pushed",
		fmt.Sprintf("workspace_id=%d", workspaceId),
		fmt.Sprintf("generation=%d", snapshot.Generation),
		fmt.Sprintf("projects=%d", len(snapshot.Projects)),
		fmt.Sprintf("conversations=%d", len(snapshot.Conversations)),
	)
	return nil
}

// SyncWorkspace pulls the pending guard observations, applies them in one
// transaction and only then acknowledges the consumed offset. Apply failures
// leave the offset untouched so the next sync retries the same lines; a
// changed workspace republishes its ownership document afterwards.
func SyncWorkspace(ctx context.Context, workspaceId int) error {
	client, err := newAgentClient()
	if err != nil {
		return err
	}
	observations, nextOffset, err := client.PullObservations(ctx, workspaceId)
	if err != nil {
		return err
	}
	changed, err := ApplyObservations(workspaceId, observations)
	if err != nil {
		return err
	}
	if err := client.AckObservations(ctx, workspaceId, nextOffset); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return pushOwnershipWithClient(ctx, client, workspaceId)
}

// ApplyObservations applies one guard batch inside a single transaction. It is
// idempotent: replaying an applied batch never duplicates rows, and it reports
// whether the replay changed the local state.
func ApplyObservations(workspaceId int, observations []Observation) (bool, error) {
	if workspaceId <= 0 || len(observations) == 0 {
		return false, nil
	}
	changed := false
	applied := 0
	skipped := 0
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		for i := range observations {
			var (
				appliedRow bool
				err        error
			)
			switch observations[i].Event {
			case ObservationProjectCreated:
				appliedRow, err = applyProjectCreated(tx, workspaceId, observations[i])
			case ObservationProjectRenamed:
				appliedRow, err = applyProjectRenamed(tx, workspaceId, observations[i])
			case ObservationConversationCreated:
				appliedRow, err = applyConversationCreated(tx, workspaceId, observations[i])
			case ObservationProjectNotFound:
				appliedRow, err = applyProjectNotFound(tx, workspaceId, observations[i])
			default:
				continue
			}
			if err != nil {
				return err
			}
			if appliedRow {
				changed = true
				applied++
			} else {
				skipped++
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	webWorkspaceAudit(
		"observations_applied",
		fmt.Sprintf("workspace_id=%d", workspaceId),
		fmt.Sprintf("count=%d", len(observations)),
		fmt.Sprintf("applied=%d", applied),
		fmt.Sprintf("skipped=%d", skipped),
		fmt.Sprintf("changed=%t", changed),
	)
	return changed, nil
}

// applyProjectCreated registers a provider-side project the guard observed
// being created under a permit.
func applyProjectCreated(tx *gorm.DB, workspaceId int, observation Observation) (bool, error) {
	externalId := strings.TrimSpace(observation.ExternalProjectId)
	if !externalProjectIdPattern.MatchString(externalId) {
		auditObservationSkipped(workspaceId, "invalid_external_project_id")
		return false, nil
	}
	if permits.matched(workspaceId, strings.TrimSpace(observation.PermitId)) {
		webWorkspaceAudit("permit_matched", fmt.Sprintf("workspace_id=%d", workspaceId))
	} else {
		webWorkspaceAudit("permit_unmatched", fmt.Sprintf("workspace_id=%d", workspaceId))
	}
	name := projectDisplayName(observation.DisplayName, observation.Slug, externalId)
	project := model.WebProject{WorkspaceId: workspaceId, Provider: DefaultProvider, ExternalProjectId: externalId, Name: name}
	created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&project)
	if created.Error != nil {
		return false, created.Error
	}
	if created.RowsAffected > 0 {
		webWorkspaceAudit("project_registered", fmt.Sprintf("workspace_id=%d", workspaceId))
		return true, nil
	}
	// The provider-side project already has a local row. Never move it into
	// another workspace: the unique index makes provider ids global.
	var existing model.WebProject
	if err := tx.Where("provider = ? AND external_project_id = ?", DefaultProvider, externalId).First(&existing).Error; err != nil {
		return false, err
	}
	if existing.WorkspaceId != workspaceId {
		auditObservationSkipped(workspaceId, "project_owned_by_another_workspace")
		return false, nil
	}
	if existing.Name == name {
		return false, nil
	}
	if err := tx.Model(&model.WebProject{}).Where("id = ?", existing.Id).Update("name", name).Error; err != nil {
		return false, err
	}
	return true, nil
}

// applyProjectRenamed updates the local mapping name of a renamed provider-side
// project; projects without a local row are ignored.
func applyProjectRenamed(tx *gorm.DB, workspaceId int, observation Observation) (bool, error) {
	externalId := strings.TrimSpace(observation.ExternalProjectId)
	if !externalProjectIdPattern.MatchString(externalId) {
		auditObservationSkipped(workspaceId, "invalid_external_project_id")
		return false, nil
	}
	var project model.WebProject
	err := tx.Where("workspace_id = ? AND provider = ? AND external_project_id = ?", workspaceId, DefaultProvider, externalId).First(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		webWorkspaceAudit("project_renamed", fmt.Sprintf("workspace_id=%d", workspaceId), "result=ignored")
		return false, nil
	}
	if err != nil {
		return false, err
	}
	name := projectDisplayName("", observation.Slug, externalId)
	if name == project.Name {
		return false, nil
	}
	if err := tx.Model(&model.WebProject{}).Where("id = ?", project.Id).Update("name", name).Error; err != nil {
		return false, err
	}
	webWorkspaceAudit("project_renamed", fmt.Sprintf("workspace_id=%d", workspaceId), "result=applied")
	return true, nil
}

// applyConversationCreated registers a provider-side conversation under its
// local project; conversations without a local project are skipped instead of
// creating an orphan row.
func applyConversationCreated(tx *gorm.DB, workspaceId int, observation Observation) (bool, error) {
	externalProjectId := strings.TrimSpace(observation.ExternalProjectId)
	externalConversationId := strings.TrimSpace(observation.ExternalConversationId)
	if !externalProjectIdPattern.MatchString(externalProjectId) || !externalConversationIdPattern.MatchString(externalConversationId) {
		auditObservationSkipped(workspaceId, "invalid_external_id")
		return false, nil
	}
	var project model.WebProject
	err := tx.Where("workspace_id = ? AND provider = ? AND external_project_id = ?", workspaceId, DefaultProvider, externalProjectId).First(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		webWorkspaceAudit("orphan_conversation_skipped", fmt.Sprintf("workspace_id=%d", workspaceId))
		return false, nil
	}
	if err != nil {
		return false, err
	}
	conversation := model.WebConversation{ProjectId: project.Id, Provider: DefaultProvider, ExternalConversationId: externalConversationId, Title: externalConversationId}
	created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&conversation)
	if created.Error != nil {
		return false, created.Error
	}
	if created.RowsAffected > 0 {
		webWorkspaceAudit("conversation_registered", fmt.Sprintf("workspace_id=%d", workspaceId))
		return true, nil
	}
	var existing model.WebConversation
	if err := tx.Where("provider = ? AND external_conversation_id = ?", DefaultProvider, externalConversationId).First(&existing).Error; err != nil {
		return false, err
	}
	if existing.ProjectId != project.Id {
		auditObservationSkipped(workspaceId, "conversation_owned_by_another_project")
		return false, nil
	}
	return false, nil
}

// applyProjectNotFound removes the local project and its conversations after
// the provider reported the project as gone. A new permit can register it
// again, mirroring the guard's fail-closed behaviour.
func applyProjectNotFound(tx *gorm.DB, workspaceId int, observation Observation) (bool, error) {
	externalId := strings.TrimSpace(observation.ExternalProjectId)
	if !externalProjectIdPattern.MatchString(externalId) {
		auditObservationSkipped(workspaceId, "invalid_external_project_id")
		return false, nil
	}
	var project model.WebProject
	err := tx.Where("workspace_id = ? AND provider = ? AND external_project_id = ?", workspaceId, DefaultProvider, externalId).First(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := tx.Where("project_id = ?", project.Id).Delete(&model.WebConversation{}).Error; err != nil {
		return false, err
	}
	if err := tx.Where("id = ?", project.Id).Delete(&model.WebProject{}).Error; err != nil {
		return false, err
	}
	webWorkspaceAudit("project_removed", fmt.Sprintf("workspace_id=%d", workspaceId), "reason=project_not_found_removed")
	return true, nil
}

// projectDisplayName prefers the explicit operator-facing name recorded with a
// creation permit, then the provider slug, and finally the stable external id.
func projectDisplayName(displayName string, slug string, externalId string) string {
	for _, candidate := range []string{displayName, slug} {
		name := strings.TrimSpace(candidate)
		if name != "" && utf8.RuneCountInString(name) <= 255 {
			return name
		}
	}
	return externalId
}
