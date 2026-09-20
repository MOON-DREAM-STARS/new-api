package guard

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
	"github.com/QuantumNous/new-api/browser-agent/internal/provider/chatgpt"
)

const (
	// DefaultStateDir is the fixed .guard directory inside the runtime
	// workspace. The agent prepares it (0700, 10001:10001); the guard only
	// reads ownership.json/permit.json and appends its own artifacts.
	DefaultStateDir = "/workspace/.guard"

	// StateDirEnv overrides DefaultStateDir when Config.StateDir is empty.
	StateDirEnv = "WW_GUARD_STATE_DIR"

	ownershipFileName      = "ownership.json"
	permitFileName         = "permit.json"
	permitConsumedFileName = "permit.consumed"
	observationsFileName   = "observations.jsonl"

	// ownershipPollInterval is the frozen re-read interval of ownership.json.
	ownershipPollInterval = 2 * time.Second

	permitKindProjectCreate = "project_create"

	// Deny reasons of the provider ownership decision. They are the audit
	// reasons of policy_deny/target_deny and never contain request payloads.
	reasonOwnershipUnavailable       = "ownership_state_unavailable"
	reasonProjectNotRegistered       = "project_not_registered"
	reasonConversationWithoutProject = "conversation_without_project"
	reasonUnknownResourceShape       = "unknown_resource_shape"
	reasonPermitNotRecorded          = "permit_consume_failed"
)

// ownershipFile mirrors the ownership.json written by the agent.
type ownershipFile struct {
	Generation    *int     `json:"generation"`
	Projects      []string `json:"projects"`
	Conversations []string `json:"conversations"`
	UpdatedAt     int64    `json:"updated_at"`
}

// permitFile mirrors the permit.json written by the agent. DisplayName is empty
// for the legacy manual flow and carries the operator-facing name when the
// creation controller is expected to drive the provider UI itself.
type permitFile struct {
	PermitID    string `json:"permit_id"`
	Kind        string `json:"kind"`
	IssuedAt    int64  `json:"issued_at"`
	ExpiresAt   int64  `json:"expires_at"`
	DisplayName string `json:"display_name"`
}

// consumedFile mirrors the permit.consumed written by the guard.
type consumedFile struct {
	PermitID   string `json:"permit_id"`
	ConsumedAt int64  `json:"consumed_at"`
}

// verdict is one ownership decision.
type verdict struct {
	Allowed bool
	Reason  string
}

// providerState is the guard's view of the .guard state directory. It starts
// without any registered resource: every provider resource navigation is denied
// until ownership.json has been read successfully.
type providerState struct {
	dir    string
	mode   policy.Mode
	logger *slog.Logger
	now    func() time.Time

	mu            sync.Mutex
	available     bool
	projects      map[string]struct{}
	conversations map[string]struct{}
	unavailable   string
	seen          map[string]struct{}
	lastSlug      map[string]string
}

func newProviderState(dir string, mode policy.Mode, logger *slog.Logger) *providerState {
	return &providerState{
		dir:           dir,
		mode:          mode,
		logger:        logger,
		now:           time.Now,
		projects:      map[string]struct{}{},
		conversations: map[string]struct{}{},
		seen:          map[string]struct{}{},
		lastSlug:      map[string]string{},
	}
}

// resolveStateDir applies the frozen precedence: explicit config, then
// WW_GUARD_STATE_DIR, then the fixed in-container path.
func resolveStateDir(explicit string) string {
	if dir := strings.TrimSpace(explicit); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv(StateDirEnv)); dir != "" {
		return dir
	}
	return DefaultStateDir
}

// refresh reloads ownership.json. A missing, unreadable or invalid state file
// leaves the guard without any registered resource, which denies every provider
// resource navigation until a later poll succeeds. The transition into that
// state is audited once per distinct reason.
func (s *providerState) refresh() {
	projects, conversations, err := readOwnership(s.dir)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.available = false
		s.projects = map[string]struct{}{}
		s.conversations = map[string]struct{}{}
		if s.unavailable != err.Error() {
			s.unavailable = err.Error()
			s.logger.Info("ownership state unavailable",
				"event", "ownership_state_unavailable",
				"component", "guard",
				"mode", string(s.mode),
				"reason", err.Error(),
			)
		}
		return
	}
	s.available = true
	s.projects = projects
	s.conversations = conversations
	s.unavailable = ""
}

// readOwnership parses ownership.json into the registered id sets. Reasons are
// fixed strings: they are written to logs and must never carry request data.
func readOwnership(dir string) (map[string]struct{}, map[string]struct{}, error) {
	data, err := os.ReadFile(filepath.Join(dir, ownershipFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, errors.New("ownership state file is missing")
		}
		return nil, nil, errors.New("ownership state file is unreadable")
	}
	var file ownershipFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, nil, errors.New("ownership state file is malformed")
	}
	if file.Generation == nil || *file.Generation < 0 {
		return nil, nil, errors.New("ownership generation is missing or negative")
	}
	projects := make(map[string]struct{}, len(file.Projects))
	for _, id := range file.Projects {
		projects[id] = struct{}{}
	}
	conversations := make(map[string]struct{}, len(file.Conversations))
	for _, id := range file.Conversations {
		conversations[id] = struct{}{}
	}
	return projects, conversations, nil
}

// pollOwnershipState keeps the registry view fresh until the guard stops.
func (g *guard) pollOwnershipState(ctx context.Context) {
	ticker := time.NewTicker(ownershipPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.state.refresh()
		}
	}
}

// evaluateDocument is the ownership decision of one provider document
// navigation: a target initial URL or a Fetch Document request that already
// passed the Phase 3 address policy. Every failure fails closed.
func (s *providerState) evaluateDocument(rawURL string) verdict {
	resource, err := chatgpt.Classify(rawURL)
	if err != nil {
		return verdict{Reason: "unparsable url"}
	}
	switch resource.Kind {
	case chatgpt.KindShell, chatgpt.KindOther:
		return verdict{Allowed: true}
	case chatgpt.KindUnknown:
		return verdict{Reason: reasonUnknownResourceShape}
	case chatgpt.KindConversationNoProject:
		return verdict{Reason: reasonConversationWithoutProject}
	case chatgpt.KindProject, chatgpt.KindConversation:
		return s.evaluateProjectNavigation(resource)
	default:
		return verdict{Reason: reasonUnknownResourceShape}
	}
}

// evaluateProjectNavigation decides one project or conversation navigation. The
// caller holds no lock; the whole decision, its permit consumption and its
// observations run under the state lock so one permit can never be spent twice.
func (s *providerState) evaluateProjectNavigation(resource chatgpt.Resource) verdict {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.available {
		return verdict{Reason: reasonOwnershipUnavailable}
	}
	if _, registered := s.projects[resource.ProjectID]; registered {
		s.observeNavigation(resource)
		return verdict{Allowed: true}
	}
	if resource.Kind != chatgpt.KindProject {
		// A conversation must always be traceable to a registered project.
		return verdict{Reason: reasonProjectNotRegistered}
	}
	permit, ok := s.activePermit()
	if !ok {
		return verdict{Reason: reasonProjectNotRegistered}
	}
	if err := s.writeConsumedPermit(permit.PermitID); err != nil {
		// Without the consumption record a second project could be allowed
		// with the same permit, so the navigation is denied instead.
		s.logger.Warn("permit consumption not recorded",
			"event", "permit_consume_failed",
			"component", "guard",
			"mode", string(s.mode),
			"reason", err.Error(),
		)
		return verdict{Reason: reasonPermitNotRecorded}
	}
	s.observeProjectCreated(resource.ProjectID, resource.Slug, permit.DisplayName, permit.PermitID)
	return verdict{Allowed: true}
}

// activePermit reads the current project creation permit. The file is read on
// demand so a freshly issued permit is honoured without waiting for the
// ownership poll. Missing, expired, already consumed or malformed permits are
// reported as absent, which denies the navigation.
func (s *providerState) activePermit() (permitFile, bool) {
	data, err := os.ReadFile(filepath.Join(s.dir, permitFileName))
	if err != nil {
		return permitFile{}, false
	}
	var permit permitFile
	if err := json.Unmarshal(data, &permit); err != nil {
		return permitFile{}, false
	}
	if permit.Kind != permitKindProjectCreate || strings.TrimSpace(permit.PermitID) == "" {
		return permitFile{}, false
	}
	if permit.ExpiresAt <= s.now().Unix() {
		return permitFile{}, false
	}
	if s.permitConsumed(permit.PermitID) {
		return permitFile{}, false
	}
	return permit, true
}

// permitConsumed fails closed through permitConsumedByDir, the shared reader of
// the single-use marker that the creation controller also respects.
func (s *providerState) permitConsumed(permitID string) bool {
	return permitConsumedByDir(s.dir, permitID)
}

// writeConsumedPermit records the single use of one permit atomically, so a
// reader only ever sees the old or the complete new file.
func (s *providerState) writeConsumedPermit(permitID string) error {
	data, err := json.Marshal(consumedFile{PermitID: permitID, ConsumedAt: s.now().Unix()})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, permitConsumedFileName+".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(s.dir, permitConsumedFileName))
}
