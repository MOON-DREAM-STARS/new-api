package guard

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/QuantumNous/new-api/browser-agent/internal/provider/chatgpt"
)

// observationRecord is one line of observations.jsonl. The agent reads the file
// and applies each event idempotently, so the field set of every event is fixed
// by the Phase 4 contract: an empty slug stays present, ids that do not belong
// to an event stay absent.
type observationRecord struct {
	Event                  string  `json:"event"`
	PermitID               string  `json:"permit_id,omitempty"`
	ExternalProjectID      string  `json:"external_project_id,omitempty"`
	ExternalConversationID string  `json:"external_conversation_id,omitempty"`
	Slug                   *string `json:"slug,omitempty"`
	ObservedAt             int64   `json:"observed_at"`
}

// observeNavigation records the provider facts of one allowed navigation to a
// registered project.
func (s *providerState) observeNavigation(resource chatgpt.Resource) {
	s.trackSlug(resource.ProjectID, resource.Slug)
	if resource.Kind == chatgpt.KindConversation {
		s.observeConversationCreated(resource.ProjectID, resource.ConversationID)
	}
}

// observeProjectCreated records the first navigation of a project that was
// allowed by a creation permit. The dedup key contains the permit id, so a
// project re-created later with a new permit is observed again.
func (s *providerState) observeProjectCreated(projectID, slug, permitID string) {
	if !s.markSeen("project_created\x00" + projectID + "\x00" + permitID) {
		return
	}
	// A project that exists again can be reported as missing later on.
	delete(s.seen, "project_not_found\x00"+projectID)
	s.lastSlug[projectID] = slug
	s.append(observationRecord{
		Event:             "project_created",
		PermitID:          permitID,
		ExternalProjectID: projectID,
		Slug:              &slug,
	})
}

// observeConversationCreated records one conversation of a registered project
// once per guard lifetime; a restart may repeat it and the agent is idempotent.
func (s *providerState) observeConversationCreated(projectID, conversationID string) {
	if !s.markSeen("conversation_created\x00" + projectID + "\x00" + conversationID) {
		return
	}
	s.append(observationRecord{
		Event:                  "conversation_created",
		ExternalProjectID:      projectID,
		ExternalConversationID: conversationID,
	})
}

// trackSlug keeps the last observed slug of a project. The first observation is
// the baseline of the process and only a different slug is a rename, which is
// also the in-memory dedup of this event: an unchanged slug produces nothing.
func (s *providerState) trackSlug(projectID, slug string) {
	previous, seen := s.lastSlug[projectID]
	s.lastSlug[projectID] = slug
	if !seen || previous == slug {
		return
	}
	s.append(observationRecord{
		Event:             "project_renamed",
		ExternalProjectID: projectID,
		Slug:              &slug,
	})
}

// observeProjectNotFound records the loss of one registered project once per
// guard lifetime.
func (s *providerState) observeProjectNotFound(projectID string) {
	if !s.markSeen("project_not_found\x00" + projectID) {
		return
	}
	s.append(observationRecord{Event: "project_not_found", ExternalProjectID: projectID})
}

// observeProjectNotFoundURL applies the Network.responseReceived rule: only a
// document response with status 404 or 410 for a project URL that the guard
// currently has registered can mean that the provider lost the project. A URL
// of a conversation is not a project observation, and a 404 of an unregistered
// project is already handled by the ownership decision itself.
func (s *providerState) observeProjectNotFoundURL(rawURL string) {
	resource, err := chatgpt.Classify(rawURL)
	if err != nil || resource.Kind != chatgpt.KindProject {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.available {
		return
	}
	if _, registered := s.projects[resource.ProjectID]; !registered {
		return
	}
	s.observeProjectNotFound(resource.ProjectID)
}

// markSeen reports whether key was new. It is the in-memory dedup of the
// observation stream; a restarted guard starts with an empty set again.
func (s *providerState) markSeen(key string) bool {
	if _, ok := s.seen[key]; ok {
		return false
	}
	s.seen[key] = struct{}{}
	return true
}

// append writes one complete JSON line with O_APPEND. A failed observation must
// never change an interception decision, so it is only logged as a warning.
func (s *providerState) append(record observationRecord) {
	record.ObservedAt = s.now().Unix()
	data, err := json.Marshal(record)
	if err != nil {
		s.warnObservationFailure(err)
		return
	}
	data = append(data, '\n')
	file, err := s.openAppend(observationsFileName)
	if err != nil {
		s.warnObservationFailure(err)
		return
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		s.warnObservationFailure(err)
	}
}

func (s *providerState) openAppend(name string) (*os.File, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(s.dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
}

func (s *providerState) warnObservationFailure(err error) {
	s.logger.Warn("observation not written",
		"event", "observation_write_failed",
		"component", "guard",
		"mode", string(s.mode),
		"reason", err.Error(),
	)
}
