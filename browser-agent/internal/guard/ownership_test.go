package guard

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

const (
	testProjectID     = "g-p-0123456789abcdef0123456789abcdef"
	testProjectID2    = "g-p-fedcba9876543210fedcba9876543210"
	testProjectID3    = "g-p-00112233445566778899aabbccddeeff"
	testConversation  = "0f8fad5b-d9cb-469f-a165-70867728950e"
	testConversation2 = "1a2b3c4d-5e6f-4a8b-9c0d-1e2f3a4b5c6d"
)

func projectURL(id string) string {
	return "https://chatgpt.com/g/" + id + "/project"
}

func writeStateFile(t *testing.T, dir, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
}

func writeOwnership(t *testing.T, dir string, generation int, projects, conversations []string) {
	t.Helper()
	writeStateFile(t, dir, ownershipFileName, map[string]any{
		"generation":    generation,
		"projects":      projects,
		"conversations": conversations,
		"updated_at":    0,
	})
}

func writePermit(t *testing.T, dir, permitID string, ttl time.Duration) {
	t.Helper()
	now := time.Now()
	writeStateFile(t, dir, permitFileName, map[string]any{
		"permit_id":  permitID,
		"kind":       permitKindProjectCreate,
		"issued_at":  now.Unix(),
		"expires_at": now.Add(ttl).Unix(),
	})
}

// sendDocument pushes one document navigation into the guard.
func sendDocument(t *testing.T, f *fakeCDP, sessionID, requestID, rawURL string) {
	t.Helper()
	f.send("Fetch.requestPaused", sessionID, map[string]any{
		"requestId":    requestID,
		"resourceType": "Document",
		"request":      map[string]any{"url": rawURL, "method": "GET"},
	})
}

func expectContinue(t *testing.T, f *fakeCDP, sessionID, requestID string) {
	t.Helper()
	cmd := f.awaitIn(sessionID, "Fetch.continueRequest")
	params := struct {
		RequestID string `json:"requestId"`
	}{}
	decodeParams(t, cmd, &params)
	assert.Equal(t, requestID, params.RequestID)
}

func expectDeny(t *testing.T, f *fakeCDP, sessionID, requestID string) {
	t.Helper()
	cmd := f.awaitIn(sessionID, "Fetch.failRequest")
	params := struct {
		RequestID   string `json:"requestId"`
		ErrorReason string `json:"errorReason"`
	}{}
	decodeParams(t, cmd, &params)
	assert.Equal(t, requestID, params.RequestID)
	assert.Equal(t, "AccessDenied", params.ErrorReason)
}

// readObservations parses observations.jsonl: every non empty line must be one
// complete JSON object, which is the append contract with the agent.
func readObservations(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, observationsFileName))
	if err != nil {
		require.ErrorIs(t, err, fs.ErrNotExist)
		return nil
	}
	require.NotEmpty(t, data, "observations.jsonl must not be empty")
	text := string(data)
	require.True(t, strings.HasSuffix(text, "\n"), "every observation line must be complete")
	records := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		require.NotEmpty(t, line, "observations.jsonl must not contain blank lines")
		record := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &record), "line is not JSON: %s", line)
		require.Contains(t, record, "event")
		require.Contains(t, record, "observed_at")
		records = append(records, record)
	}
	return records
}

// awaitObservations waits for at least want observations. Observation writes
// happen after the interception decision, so tests cannot await a CDP command
// for them.
func awaitObservations(t *testing.T, dir string, want int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		records := readObservations(t, dir)
		if len(records) >= want {
			return records
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d observations, saw %d", want, len(records))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func recordsOfEvent(records []map[string]any, event string) []map[string]any {
	var found []map[string]any
	for _, record := range records {
		if record["event"] == event {
			found = append(found, record)
		}
	}
	return found
}

// requireKeys pins the exact field set of one observation: the agent applies
// the events by field, so a missing or extra field is a contract change.
func requireKeys(t *testing.T, record map[string]any, keys ...string) {
	t.Helper()
	require.Len(t, record, len(keys), "unexpected observation fields: %v", record)
	for _, key := range keys {
		require.Contains(t, record, key)
	}
}

func readConsumedPermit(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, permitConsumedFileName))
	require.NoError(t, err)
	record := map[string]any{}
	require.NoError(t, json.Unmarshal(data, &record))
	return record
}

func requireNoFile(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.ErrorIs(t, err, fs.ErrNotExist, "expected %s to be absent", filepath.Base(path))
}

func TestGuardAllowsRegisteredProjectNavigation(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	dir := t.TempDir()
	writeOwnership(t, dir, 3, []string{testProjectID}, nil)

	run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
	attachPage(t, f, "session-1", "target-1")

	sendDocument(t, f, "session-1", "request-1", projectURL(testProjectID))
	expectContinue(t, f, "session-1", "request-1")

	assert.False(t, logs.hasEvent("policy_deny"), "a registered project must not be audited as a deny")
	assert.False(t, logs.hasEvent("ownership_state_unavailable"))
	assert.Empty(t, readObservations(t, dir), "a plain project visit is not an observation")
	run.assertRunning(200 * time.Millisecond)
}

func TestGuardDeniesUnregisteredProjectNavigation(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	dir := t.TempDir()
	writeOwnership(t, dir, 3, []string{testProjectID2}, nil)

	run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
	attachPage(t, f, "session-1", "target-1")

	sendDocument(t, f, "session-1", "request-1", projectURL(testProjectID))
	expectDeny(t, f, "session-1", "request-1")

	entry := logs.awaitEntry(t, "policy_deny")
	assert.Equal(t, "guard", entry["component"])
	assert.Equal(t, "LOCKED", entry["mode"])
	assert.Equal(t, "chatgpt.com", entry["host"])
	assert.Equal(t, "Document", entry["resource_type"])
	assert.Equal(t, reasonProjectNotRegistered, entry["reason"])
	assert.NotContains(t, logs.String(), testProjectID, "logs must not carry the project path or id")
	assert.Empty(t, readObservations(t, dir))
	run.assertRunning(200 * time.Millisecond)
}

func TestGuardConsumesOnePermitForOneProject(t *testing.T) {
	t.Run("valid permit allows exactly one project", func(t *testing.T) {
		logs := &logBuffer{}
		f := newFakeCDP(t)
		dir := t.TempDir()
		writeOwnership(t, dir, 0, nil, nil)
		writePermit(t, dir, "permit-0123456789abcdef", 5*time.Minute)

		run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
		attachPage(t, f, "session-1", "target-1")

		sendDocument(t, f, "session-1", "request-1", "https://chatgpt.com/g/"+testProjectID+"-my-project/project")
		expectContinue(t, f, "session-1", "request-1")

		consumed := readConsumedPermit(t, dir)
		assert.Equal(t, "permit-0123456789abcdef", consumed["permit_id"])
		assert.NotZero(t, consumed["consumed_at"])

		records := awaitObservations(t, dir, 1)
		require.Len(t, records, 1)
		requireKeys(t, records[0], "event", "permit_id", "external_project_id", "slug", "observed_at")
		assert.Equal(t, "project_created", records[0]["event"])
		assert.Equal(t, "permit-0123456789abcdef", records[0]["permit_id"])
		assert.Equal(t, testProjectID, records[0]["external_project_id"])
		assert.Equal(t, "my-project", records[0]["slug"])

		// The permit is spent: a second unregistered project is denied.
		sendDocument(t, f, "session-1", "request-2", projectURL(testProjectID2))
		expectDeny(t, f, "session-1", "request-2")
		entry := logs.awaitEntry(t, "policy_deny")
		assert.Equal(t, reasonProjectNotRegistered, entry["reason"])
		assert.Len(t, readObservations(t, dir), 1, "a denied navigation must not observe a project")
		assert.Equal(t, "permit-0123456789abcdef", readConsumedPermit(t, dir)["permit_id"])
		run.assertRunning(200 * time.Millisecond)
	})

	t.Run("permit allows a project without a slug", func(t *testing.T) {
		logs := &logBuffer{}
		f := newFakeCDP(t)
		dir := t.TempDir()
		writeOwnership(t, dir, 0, nil, nil)
		writePermit(t, dir, "permit-noslug-00000001", 5*time.Minute)

		startGuardWithState(t, f, policy.ModeLocked, logs, dir)
		attachPage(t, f, "session-1", "target-1")

		sendDocument(t, f, "session-1", "request-1", projectURL(testProjectID))
		expectContinue(t, f, "session-1", "request-1")

		records := awaitObservations(t, dir, 1)
		require.Len(t, records, 1)
		requireKeys(t, records[0], "event", "permit_id", "external_project_id", "slug", "observed_at")
		assert.Equal(t, "", records[0]["slug"], "a project without a slug keeps the empty slug field")
	})

	t.Run("expired permit is denied", func(t *testing.T) {
		logs := &logBuffer{}
		f := newFakeCDP(t)
		dir := t.TempDir()
		writeOwnership(t, dir, 0, nil, nil)
		writePermit(t, dir, "permit-expired-0001", -time.Minute)

		startGuardWithState(t, f, policy.ModeLocked, logs, dir)
		attachPage(t, f, "session-1", "target-1")

		sendDocument(t, f, "session-1", "request-1", projectURL(testProjectID))
		expectDeny(t, f, "session-1", "request-1")
		entry := logs.awaitEntry(t, "policy_deny")
		assert.Equal(t, reasonProjectNotRegistered, entry["reason"])
		requireNoFile(t, filepath.Join(dir, permitConsumedFileName))
		assert.Empty(t, readObservations(t, dir))
	})

	t.Run("consumed permit is denied", func(t *testing.T) {
		logs := &logBuffer{}
		f := newFakeCDP(t)
		dir := t.TempDir()
		writeOwnership(t, dir, 0, nil, nil)
		writePermit(t, dir, "permit-consumed-0001", 5*time.Minute)
		writeStateFile(t, dir, permitConsumedFileName, map[string]any{
			"permit_id":   "permit-consumed-0001",
			"consumed_at": time.Now().Unix(),
		})

		startGuardWithState(t, f, policy.ModeLocked, logs, dir)
		attachPage(t, f, "session-1", "target-1")

		sendDocument(t, f, "session-1", "request-1", projectURL(testProjectID))
		expectDeny(t, f, "session-1", "request-1")
		entry := logs.awaitEntry(t, "policy_deny")
		assert.Equal(t, reasonProjectNotRegistered, entry["reason"])
		assert.Empty(t, readObservations(t, dir))
	})
}

func TestGuardObservesConversationOfRegisteredProject(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	dir := t.TempDir()
	writeOwnership(t, dir, 4, []string{testProjectID}, []string{testConversation})

	run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
	attachPage(t, f, "session-1", "target-1")

	conversation := "https://chatgpt.com/g/" + testProjectID + "-my-project/c/" + testConversation
	sendDocument(t, f, "session-1", "request-1", conversation)
	expectContinue(t, f, "session-1", "request-1")

	records := awaitObservations(t, dir, 1)
	require.Len(t, records, 1)
	requireKeys(t, records[0], "event", "external_project_id", "external_conversation_id", "observed_at")
	assert.Equal(t, "conversation_created", records[0]["event"])
	assert.Equal(t, testProjectID, records[0]["external_project_id"])
	assert.Equal(t, testConversation, records[0]["external_conversation_id"])

	// The same conversation is observed once per guard lifetime.
	sendDocument(t, f, "session-1", "request-2", conversation)
	expectContinue(t, f, "session-1", "request-2")
	assert.Len(t, readObservations(t, dir), 1)

	// A later navigation of the same project with another slug is a rename.
	sendDocument(t, f, "session-1", "request-3", "https://chatgpt.com/g/"+testProjectID+"-renamed/project")
	expectContinue(t, f, "session-1", "request-3")

	records = awaitObservations(t, dir, 2)
	renames := recordsOfEvent(records, "project_renamed")
	require.Len(t, renames, 1)
	requireKeys(t, renames[0], "event", "external_project_id", "slug", "observed_at")
	assert.Equal(t, testProjectID, renames[0]["external_project_id"])
	assert.Equal(t, "renamed", renames[0]["slug"])

	// The rename is deduplicated in memory as long as the slug does not change.
	sendDocument(t, f, "session-1", "request-4", "https://chatgpt.com/g/"+testProjectID+"-renamed/project")
	expectContinue(t, f, "session-1", "request-4")
	assert.Len(t, readObservations(t, dir), 2)
	assert.False(t, logs.hasEvent("policy_deny"))
	run.assertRunning(200 * time.Millisecond)
}

func TestGuardDeniesConversationsWithoutRegisteredProject(t *testing.T) {
	t.Run("conversation without project is always denied", func(t *testing.T) {
		logs := &logBuffer{}
		f := newFakeCDP(t)
		dir := t.TempDir()
		writeOwnership(t, dir, 2, []string{testProjectID}, []string{testConversation})

		startGuardWithState(t, f, policy.ModeLocked, logs, dir)
		attachPage(t, f, "session-1", "target-1")

		sendDocument(t, f, "session-1", "request-1", "https://chatgpt.com/c/"+testConversation)
		expectDeny(t, f, "session-1", "request-1")
		entry := logs.awaitEntry(t, "policy_deny")
		assert.Equal(t, reasonConversationWithoutProject, entry["reason"])
		assert.Empty(t, readObservations(t, dir))
	})

	t.Run("conversation of an unregistered project is denied", func(t *testing.T) {
		logs := &logBuffer{}
		f := newFakeCDP(t)
		dir := t.TempDir()
		writeOwnership(t, dir, 2, []string{testProjectID2}, nil)

		startGuardWithState(t, f, policy.ModeLocked, logs, dir)
		attachPage(t, f, "session-1", "target-1")

		sendDocument(t, f, "session-1", "request-1", "https://chatgpt.com/g/"+testProjectID+"/c/"+testConversation)
		expectDeny(t, f, "session-1", "request-1")
		entry := logs.awaitEntry(t, "policy_deny")
		assert.Equal(t, reasonProjectNotRegistered, entry["reason"])
		assert.Empty(t, readObservations(t, dir))
	})
}

func TestGuardDeniesUnknownResourceShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
	}{
		{name: "short project id", url: "https://chatgpt.com/g/g-p-0123/project"},
		{name: "unknown project subpath", url: "https://chatgpt.com/g/" + testProjectID + "/settings"},
		{name: "conversation id without uuid", url: "https://chatgpt.com/c/123"},
		{name: "double trailing slash", url: "https://chatgpt.com/g/" + testProjectID + "/project//"},
		{name: "encoded path separator", url: "https://chatgpt.com/g/" + testProjectID + "%2Fproject"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &logBuffer{}
			f := newFakeCDP(t)
			dir := t.TempDir()
			writeOwnership(t, dir, 2, []string{testProjectID}, nil)

			startGuardWithState(t, f, policy.ModeLocked, logs, dir)
			attachPage(t, f, "session-1", "target-1")

			sendDocument(t, f, "session-1", "request-1", tc.url)
			expectDeny(t, f, "session-1", "request-1")
			entry := logs.awaitEntry(t, "policy_deny")
			assert.Equal(t, reasonUnknownResourceShape, entry["reason"])
			assert.Empty(t, readObservations(t, dir))
		})
	}
}

func TestGuardDeniesProviderResourcesWhenOwnershipUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(t *testing.T, dir string)
	}{
		{name: "missing state file", write: func(t *testing.T, dir string) {}},
		{name: "malformed state file", write: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, ownershipFileName), []byte("{not json"), 0o600))
		}},
		{name: "negative generation", write: func(t *testing.T, dir string) {
			writeOwnership(t, dir, -1, []string{testProjectID}, nil)
		}},
		{name: "generation missing", write: func(t *testing.T, dir string) {
			writeStateFile(t, dir, ownershipFileName, map[string]any{"projects": []string{testProjectID}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &logBuffer{}
			f := newFakeCDP(t)
			dir := t.TempDir()
			tc.write(t, dir)

			run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
			attachPage(t, f, "session-1", "target-1")

			sendDocument(t, f, "session-1", "request-1", projectURL(testProjectID))
			expectDeny(t, f, "session-1", "request-1")
			entry := logs.awaitEntry(t, "policy_deny")
			assert.Equal(t, reasonOwnershipUnavailable, entry["reason"])

			unavailable := logs.awaitEntry(t, "ownership_state_unavailable")
			assert.Equal(t, "guard", unavailable["component"])
			assert.Equal(t, "LOCKED", unavailable["mode"])
			assert.NotEmpty(t, unavailable["reason"])
			assert.NotContains(t, logs.String(), testProjectID, "logs must not carry the project path or id")
			assert.Empty(t, readObservations(t, dir))

			// The provider shell carries no resource and stays reachable.
			sendDocument(t, f, "session-1", "request-2", "https://chatgpt.com/")
			expectContinue(t, f, "session-1", "request-2")
			run.assertRunning(200 * time.Millisecond)
		})
	}
}

func TestGuardClosesForeignProjectPopup(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	dir := t.TempDir()
	writeOwnership(t, dir, 2, []string{testProjectID2}, nil)

	run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)

	popupURL := projectURL(testProjectID) + "?from=popup"
	f.send("Target.attachedToTarget", "", map[string]any{
		"sessionId": "session-2",
		"targetInfo": map[string]any{
			"targetId": "target-2",
			"type":     "page",
			"url":      popupURL,
		},
		"waitingForDebugger": true,
	})

	cmd := f.awaitIn("", "Target.closeTarget")
	params := struct {
		TargetID string `json:"targetId"`
	}{}
	decodeParams(t, cmd, &params)
	assert.Equal(t, "target-2", params.TargetID)

	entry := logs.awaitEntry(t, "target_deny")
	assert.Equal(t, "guard", entry["component"])
	assert.Equal(t, "LOCKED", entry["mode"])
	assert.Equal(t, "chatgpt.com", entry["host"])
	assert.Equal(t, reasonProjectNotRegistered, entry["reason"])
	assert.NotContains(t, logs.String(), testProjectID)
	assert.NotContains(t, logs.String(), "from=popup")

	f.drain(200 * time.Millisecond)
	assert.False(t, f.hasCommand("session-2", "Fetch.enable"), "a closed popup must not be armed")
	assert.Empty(t, readObservations(t, dir))
	run.assertRunning(200 * time.Millisecond)
}

func TestGuardObservesProjectNotFoundFromDocumentErrors(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	dir := t.TempDir()
	writeOwnership(t, dir, 7, []string{testProjectID, testProjectID2, testProjectID3}, nil)

	run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
	attachPage(t, f, "session-1", "target-1")

	f.awaitIn("session-1", "Network.enable")

	response := func(requestID, url string, status int, resourceType string) {
		f.send("Network.responseReceived", "session-1", map[string]any{
			"requestId": requestID,
			"type":      resourceType,
			"response":  map[string]any{"url": url, "status": status},
		})
	}

	// Everything that must not be observed is sent first; the final 404 of a
	// registered project is the barrier that proves all of it was processed.
	response("r1", projectURL(testProjectID), 200, "Document")
	response("r2", projectURL(testProjectID), 404, "XHR")
	response("r3", projectURL("g-p-ffffffffffffffffffffffffffffffff"), 404, "Document")
	response("r4", "https://chatgpt.com/g/"+testProjectID+"/c/"+testConversation, 404, "Document")
	response("r5", projectURL(testProjectID2), 404, "Document")

	records := awaitObservations(t, dir, 1)
	require.Len(t, records, 1)
	requireKeys(t, records[0], "event", "external_project_id", "observed_at")
	assert.Equal(t, "project_not_found", records[0]["event"])
	assert.Equal(t, testProjectID2, records[0]["external_project_id"])

	// 410 is observed as well and a repeated 404 of the same project is not.
	response("r6", projectURL(testProjectID), 410, "Document")
	response("r7", projectURL(testProjectID2), 404, "Document")
	response("r8", projectURL(testProjectID3), 410, "Document")

	records = awaitObservations(t, dir, 3)
	require.Len(t, recordsOfEvent(records, "project_not_found"), 3)
	assert.Equal(t, testProjectID, records[1]["external_project_id"])
	assert.Equal(t, testProjectID3, records[2]["external_project_id"])
	assert.NotContains(t, logs.String(), testProjectID)
	run.assertRunning(200 * time.Millisecond)
}

func TestGuardAppendsObservationsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	writeOwnership(t, dir, 9, []string{testProjectID}, []string{testConversation, testConversation2})

	first := newFakeCDP(t)
	firstRun := startGuardWithState(t, first, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, first, "session-1", "target-1")

	sendDocument(t, first, "session-1", "request-1", "https://chatgpt.com/g/"+testProjectID+"/c/"+testConversation)
	expectContinue(t, first, "session-1", "request-1")
	sendDocument(t, first, "session-1", "request-2", "https://chatgpt.com/g/"+testProjectID+"/c/"+testConversation2)
	expectContinue(t, first, "session-1", "request-2")

	records := awaitObservations(t, dir, 2)
	require.Len(t, records, 2)
	assert.Equal(t, "conversation_created", records[0]["event"])
	assert.Equal(t, "conversation_created", records[1]["event"])

	firstRun.cancel()
	require.Error(t, firstRun.wait(), "a cancelled guard stops")

	// A restarted guard deduplicates in memory only: the same conversation is
	// observed again and the control plane applies it idempotently.
	second := newFakeCDP(t)
	secondRun := startGuardWithState(t, second, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, second, "session-1", "target-1")

	sendDocument(t, second, "session-1", "request-1", "https://chatgpt.com/g/"+testProjectID+"/c/"+testConversation)
	expectContinue(t, second, "session-1", "request-1")

	records = awaitObservations(t, dir, 3)
	require.Len(t, records, 3)
	assert.Equal(t, testConversation, records[2]["external_conversation_id"])
	secondRun.assertRunning(200 * time.Millisecond)
}

func TestGuardToleratesNetworkDomainFailure(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	dir := t.TempDir()
	writeOwnership(t, dir, 1, []string{testProjectID}, nil)
	f.failNext("Network.enable", &cdpError{Code: -32601, Message: "method not found"})

	run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
	attachPage(t, f, "session-1", "target-1")

	entry := logs.awaitEntry(t, "network_domain_unavailable")
	assert.Equal(t, "guard", entry["component"])
	assert.Equal(t, "project_not_found observation degraded", entry["reason"])
	run.assertRunning(300 * time.Millisecond)

	// Fetch enforcement is untouched by the degraded observation domain.
	sendDocument(t, f, "session-1", "request-1", projectURL(testProjectID))
	expectContinue(t, f, "session-1", "request-1")
	sendDocument(t, f, "session-1", "request-2", projectURL(testProjectID2))
	expectDeny(t, f, "session-1", "request-2")
}

func TestGuardKeepsEnforcementWhenObservationWriteFails(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	dir := t.TempDir()
	writeOwnership(t, dir, 1, []string{testProjectID}, nil)
	// A directory in place of the observation file makes every append fail.
	require.NoError(t, os.Mkdir(filepath.Join(dir, observationsFileName), 0o700))

	run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
	attachPage(t, f, "session-1", "target-1")

	sendDocument(t, f, "session-1", "request-1", "https://chatgpt.com/g/"+testProjectID+"/c/"+testConversation)
	expectContinue(t, f, "session-1", "request-1")

	entry := logs.awaitEntry(t, "observation_write_failed")
	assert.Equal(t, "guard", entry["component"])
	assert.Equal(t, "LOCKED", entry["mode"])
	assert.NotEmpty(t, entry["reason"])

	// Enforcement never depends on the observation stream: the registered
	// project stays reachable and an unregistered one stays denied.
	sendDocument(t, f, "session-1", "request-2", projectURL(testProjectID))
	expectContinue(t, f, "session-1", "request-2")
	sendDocument(t, f, "session-1", "request-3", projectURL(testProjectID2))
	expectDeny(t, f, "session-1", "request-3")
	run.assertRunning(200 * time.Millisecond)
}

func TestGuardRefreshesOwnershipOnPoll(t *testing.T) {
	logs := &logBuffer{}
	f := newFakeCDP(t)
	dir := t.TempDir()
	writeOwnership(t, dir, 1, nil, nil)

	run := startGuardWithState(t, f, policy.ModeLocked, logs, dir)
	attachPage(t, f, "session-1", "target-1")

	sendDocument(t, f, "session-1", "request-1", projectURL(testProjectID))
	expectDeny(t, f, "session-1", "request-1")

	writeOwnership(t, dir, 2, []string{testProjectID}, nil)
	time.Sleep(ownershipPollInterval + 400*time.Millisecond)

	sendDocument(t, f, "session-1", "request-2", projectURL(testProjectID))
	expectContinue(t, f, "session-1", "request-2")
	run.assertRunning(200 * time.Millisecond)
}

func TestResolveStateDir(t *testing.T) {
	t.Setenv(StateDirEnv, "")
	assert.Equal(t, DefaultStateDir, resolveStateDir(""))
	assert.Equal(t, "/tmp/guard-state", resolveStateDir("/tmp/guard-state"))

	t.Setenv(StateDirEnv, "/tmp/from-env")
	assert.Equal(t, "/tmp/from-env", resolveStateDir(""))
	assert.Equal(t, "/tmp/guard-state", resolveStateDir("/tmp/guard-state"))
}
