package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

func writeNavigationCommand(t *testing.T, dir string, command navigationCommand) {
	t.Helper()
	data, err := json.Marshal(command)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, navigationCommandFileName), data, 0o644))
}

func awaitNavigationStatus(t *testing.T, dir string, wantID int64) navigationStatus {
	t.Helper()
	return awaitNavigationStatusMatch(t, dir, wantID, func(navigationStatus) bool { return true })
}

func awaitNavigationStatusMatch(t *testing.T, dir string, wantID int64, match func(navigationStatus) bool) navigationStatus {
	t.Helper()
	path := filepath.Join(dir, navigationStatusFileName)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var status navigationStatus
			if json.Unmarshal(data, &status) == nil && status.ID == wantID && match(status) {
				return status
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for navigation status id %d at %s", wantID, path)
	return navigationStatus{}
}

func respondNavigationHistory(t *testing.T, f *fakeCDP, command cdpMessage, currentIndex int, entries ...int64) {
	t.Helper()
	encodedEntries := make([]map[string]any, 0, len(entries))
	for _, entryID := range entries {
		encodedEntries = append(encodedEntries, map[string]any{"id": entryID})
	}
	f.write(f.conn, map[string]any{
		"id":        command.ID,
		"sessionId": command.SessionID,
		"result": map[string]any{
			"currentIndex": currentIndex,
			"entries":      encodedEntries,
		},
	})
}

func consumeInitialNavigation(t *testing.T, f *fakeCDP, dir string) {
	t.Helper()
	f.awaitIn("session-1", "Page.getNavigationHistory")
	awaitNavigationStatus(t, dir, 0)
}

func countBufferedMethod(f *fakeCDP, method string) int {
	count := 0
	for _, command := range f.buffered {
		if command.Method == method {
			count++
		}
	}
	return count
}

func TestNavigationBackExecutesCommandAndDeduplicatesByID(t *testing.T) {
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")
	consumeInitialNavigation(t, f, dir)

	f.silenceNext("Page.getNavigationHistory")
	writeNavigationCommand(t, dir, navigationCommand{ID: 1, Action: "back", RequestedAt: 100})
	historyCommand := f.awaitIn("session-1", "Page.getNavigationHistory")
	respondNavigationHistory(t, f, historyCommand, 1, 10, 20)

	navigateCommand := f.awaitIn("session-1", "Page.navigateToHistoryEntry")
	params := struct {
		EntryID int64 `json:"entryId"`
	}{}
	decodeParams(t, navigateCommand, &params)
	assert.Equal(t, int64(10), params.EntryID)
	awaitNavigationStatus(t, dir, 1)

	// The command file remains in place, but the same id must not execute a
	// second navigation command.
	f.drain(700 * time.Millisecond)
	assert.Equal(t, 1, countBufferedMethod(f, "Page.navigateToHistoryEntry"))
	run.assertRunning(200 * time.Millisecond)
}

func TestNavigationProjectOpenNavigatesToAuthorizedProviderProject(t *testing.T) {
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")
	consumeInitialNavigation(t, f, dir)

	writeNavigationCommand(t, dir, navigationCommand{
		ID:          1,
		Action:      "project",
		ProjectID:   testProjectID,
		RequestedAt: 100,
	})
	navigateCommand := f.awaitIn("session-1", "Page.navigate")
	var params struct {
		URL string `json:"url"`
	}
	decodeParams(t, navigateCommand, &params)
	assert.Equal(t, "https://chatgpt.com/g/"+testProjectID+"/project", params.URL)

	f.silenceNext("Page.getNavigationHistory")
	historyCommand := f.awaitIn("session-1", "Page.getNavigationHistory")
	respondNavigationHistory(t, f, historyCommand, 1, 10, 20)
	run.assertRunning(200 * time.Millisecond)
}

func TestNavigationStateAndUnknownActionWriteReceipts(t *testing.T) {
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")
	consumeInitialNavigation(t, f, dir)

	f.silenceNext("Page.getNavigationHistory")
	writeNavigationCommand(t, dir, navigationCommand{ID: 2, Action: "state", RequestedAt: 101})
	stateCommand := f.awaitIn("session-1", "Page.getNavigationHistory")
	respondNavigationHistory(t, f, stateCommand, 1, 10, 20, 30)
	state := awaitNavigationStatus(t, dir, 2)
	assert.True(t, state.CanGoBack)
	assert.True(t, state.CanGoForward)

	f.silenceNext("Page.getNavigationHistory")
	writeNavigationCommand(t, dir, navigationCommand{ID: 3, Action: "unknown", RequestedAt: 102})
	unknownCommand := f.awaitIn("session-1", "Page.getNavigationHistory")
	respondNavigationHistory(t, f, unknownCommand, 0, 10, 20)
	unknown := awaitNavigationStatus(t, dir, 3)
	assert.False(t, unknown.CanGoBack)
	assert.True(t, unknown.CanGoForward)

	f.drain(400 * time.Millisecond)
	assert.Equal(t, 0, countBufferedMethod(f, "Page.navigateToHistoryEntry"))
	assert.Equal(t, 0, countBufferedMethod(f, "Page.reload"))
	run.assertRunning(200 * time.Millisecond)
}

func TestNavigationEventRefreshWritesStatusWithoutStoppingGuard(t *testing.T) {
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")
	consumeInitialNavigation(t, f, dir)

	f.silenceNext("Page.getNavigationHistory")
	f.send("Page.navigatedWithinDocument", "session-1", map[string]any{"url": "https://chatgpt.com/"})
	historyCommand := f.awaitIn("session-1", "Page.getNavigationHistory")
	respondNavigationHistory(t, f, historyCommand, 0, 10, 20)

	status := awaitNavigationStatusMatch(t, dir, 0, func(status navigationStatus) bool { return status.CanGoForward })
	assert.False(t, status.CanGoBack)
	assert.True(t, status.CanGoForward)
	run.assertRunning(300 * time.Millisecond)
}

func TestSameDocumentProjectNavigationConsumesPermit(t *testing.T) {
	dir := t.TempDir()
	writeOwnership(t, dir, 0, nil, nil)
	writePermit(t, dir, "permit-same-doc", 5*time.Minute)
	f := newFakeCDP(t)
	run := startGuardWithPageHealth(t, f, &logBuffer{}, dir, "https://chatgpt.com/", nil)
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")
	sendMainFrameSuccess(t, f)

	projectURL := "https://chatgpt.com/g/" + testProjectID + "-same-doc/project"
	f.send("Page.navigatedWithinDocument", "session-1", map[string]any{
		"frameId": "frame-main",
		"url":     projectURL,
	})

	require.Eventually(t, func() bool {
		return permitConsumedByDir(dir, "permit-same-doc")
	}, 2*time.Second, 10*time.Millisecond)
	records := awaitObservations(t, dir, 1)
	assert.Equal(t, "project_created", records[0]["event"])
	assert.Equal(t, testProjectID, records[0]["external_project_id"])
	assert.Equal(t, "permit-same-doc", records[0]["permit_id"])
	run.assertRunning(100 * time.Millisecond)
}

func TestSameDocumentSubframeNavigationDoesNotConsumePermit(t *testing.T) {
	dir := t.TempDir()
	writeOwnership(t, dir, 0, nil, nil)
	writePermit(t, dir, "permit-same-doc", 5*time.Minute)
	f := newFakeCDP(t)
	run := startGuardWithPageHealth(t, f, &logBuffer{}, dir, "https://chatgpt.com/", nil)
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")
	sendMainFrameSuccess(t, f)

	f.send("Page.navigatedWithinDocument", "session-1", map[string]any{
		"frameId": "frame-child",
		"url":     "https://chatgpt.com/g/" + testProjectID + "/project",
	})
	f.drain(100 * time.Millisecond)

	assert.False(t, permitConsumedByDir(dir, "permit-same-doc"))
	assert.Empty(t, readObservations(t, dir))
	run.assertRunning(100 * time.Millisecond)
}

func TestDeniedSameDocumentNavigationRecoversToStartURL(t *testing.T) {
	dir := t.TempDir()
	writeOwnership(t, dir, 0, nil, nil)
	logs := &logBuffer{}
	f := newFakeCDP(t)
	run := startGuardWithPageHealth(t, f, logs, dir, "https://chatgpt.com/", nil)
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")
	sendMainFrameSuccess(t, f)

	f.send("Page.navigatedWithinDocument", "session-1", map[string]any{
		"frameId": "frame-main",
		"url":     "https://chatgpt.com/g/" + testProjectID + "/project",
	})

	navigate := f.awaitIn("session-1", "Page.navigate")
	var params struct {
		URL string `json:"url"`
	}
	decodeParams(t, navigate, &params)
	assert.Equal(t, "https://chatgpt.com/", params.URL)
	entry := logs.awaitEntry(t, "policy_deny")
	assert.Equal(t, reasonProjectNotRegistered, entry["reason"])
	run.assertRunning(100 * time.Millisecond)
}
func TestNavigationPolicyStillBlocksDocumentRequests(t *testing.T) {
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")
	consumeInitialNavigation(t, f, dir)

	sendDocument(t, f, "session-1", "request-1", "https://evil.example.com/steal")
	expectDeny(t, f, "session-1", "request-1")
	run.assertRunning(200 * time.Millisecond)
}

func TestNavigationPrefersAppSessionAndKeepsItAfterPopupDetach(t *testing.T) {
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")
	consumeInitialNavigation(t, f, dir)

	attachPage(t, f, "session-popup", "target-popup")
	f.awaitIn("session-popup", "Page.getNavigationHistory")

	f.silenceNext("Page.getNavigationHistory")
	writeNavigationCommand(t, dir, navigationCommand{ID: 1, Action: "back", RequestedAt: 201})
	historyCommand := f.awaitIn("session-1", "Page.getNavigationHistory")
	respondNavigationHistory(t, f, historyCommand, 1, 10, 20)
	navigateCommand := f.awaitIn("session-1", "Page.navigateToHistoryEntry")
	assert.Equal(t, "session-1", navigateCommand.SessionID)
	awaitNavigationStatus(t, dir, 1)

	// Synchronise with the event loop after detaching the popup, then prove the
	// app session remains the command target.
	f.send("Target.detachedFromTarget", "", map[string]any{
		"sessionId": "session-popup",
		"targetId":  "target-popup",
	})
	f.send("Page.navigatedWithinDocument", "session-sync", map[string]any{"url": "https://chatgpt.com/"})
	f.awaitIn("session-sync", "Page.getNavigationHistory")

	f.silenceNext("Page.getNavigationHistory")
	writeNavigationCommand(t, dir, navigationCommand{ID: 2, Action: "back", RequestedAt: 202})
	secondHistory := f.awaitIn("session-1", "Page.getNavigationHistory")
	respondNavigationHistory(t, f, secondHistory, 1, 10, 20)
	secondNavigate := f.awaitIn("session-1", "Page.navigateToHistoryEntry")
	assert.Equal(t, "session-1", secondNavigate.SessionID)
	awaitNavigationStatus(t, dir, 2)
	run.assertRunning(200 * time.Millisecond)
}

func TestNavigationSessionOrderingAndFallback(t *testing.T) {
	controller := newNavigationController(t.TempDir(), nil, newTestLogger(&logBuffer{}))
	controller.setSession("session-main")
	controller.setSession("session-popup")
	controller.setSession("session-main")
	assert.Equal(t, "session-main", controller.currentSession(), "duplicate attach must not reorder sessions")

	controller.clearSession("session-popup")
	assert.Equal(t, "session-main", controller.currentSession(), "clearing a popup must keep the app session")

	controller.clearSession("session-main")
	assert.Empty(t, controller.currentSession())

	controller.setSession("session-main")
	controller.setSession("session-popup")
	controller.clearSession("session-main")
	assert.Equal(t, "session-popup", controller.currentSession(), "detaching the app session must fall back to the remaining page")
	controller.clearSession("session-popup")
	assert.Empty(t, controller.currentSession())
}
