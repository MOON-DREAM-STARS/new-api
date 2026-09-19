package guard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

func startGuardWithPageHealth(t *testing.T, f *fakeCDP, logs *logBuffer, stateDir, startURL string, delays []time.Duration) *guardRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	run := &guardRun{t: t, cancel: cancel, finished: make(chan struct{})}
	go func() {
		err := Run(ctx, Config{
			CDPURL:      f.server.URL,
			Mode:        policy.ModeLocked,
			Logger:      newTestLogger(logs),
			StateDir:    stateDir,
			StartURL:    startURL,
			retryDelays: delays,
		})
		run.mu.Lock()
		run.err = err
		run.mu.Unlock()
		close(run.finished)
	}()
	f.await("Target.setDiscoverTargets")
	f.await("Target.setAutoAttach")
	f.await("Browser.setDownloadBehavior")
	t.Cleanup(func() {
		cancel()
		run.wait()
	})
	return run
}

func sendMainDocumentFailure(t *testing.T, f *fakeCDP, requestID, rawError string) {
	t.Helper()
	f.send("Network.requestWillBeSent", "session-1", map[string]any{
		"requestId": requestID,
		"type":      "Document",
		"frameId":   "frame-main",
		"request":   map[string]any{"url": "https://chatgpt.com/"},
	})
	f.send("Network.loadingFailed", "session-1", map[string]any{
		"requestId": requestID,
		"type":      "Document",
		"errorText": rawError,
	})
}

func sendMainFrameSuccess(t *testing.T, f *fakeCDP) {
	t.Helper()
	f.send("Page.frameNavigated", "session-1", map[string]any{
		"frame": map[string]any{
			"id":  "frame-main",
			"url": "https://chatgpt.com/",
		},
	})
}

func TestPageHealthRetriesThenRecovers(t *testing.T) {
	logs := &logBuffer{}
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithPageHealth(t, f, logs, dir, "https://chatgpt.com/", []time.Duration{15 * time.Millisecond, 15 * time.Millisecond, 15 * time.Millisecond})
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")
	sendMainFrameSuccess(t, f)

	sendMainDocumentFailure(t, f, "request-1", "net::ERR_TUNNEL_CONNECTION_FAILED")
	retry := f.awaitIn("session-1", "Page.navigate")
	params := struct {
		URL string `json:"url"`
	}{}
	decodeParams(t, retry, &params)
	assert.Equal(t, "https://chatgpt.com/", params.URL)

	status := awaitNavigationStatusMatch(t, dir, 0, func(status navigationStatus) bool {
		return status.PageState == pageStateRetrying && status.PageAttempts == 1
	})
	assert.Equal(t, "ERR_TUNNEL_CONNECTION_FAILED", status.PageError)

	sendMainFrameSuccess(t, f)
	status = awaitNavigationStatusMatch(t, dir, 0, func(status navigationStatus) bool {
		return status.PageState == pageStateReady
	})
	assert.Zero(t, status.PageAttempts)
	assert.Empty(t, status.PageError)
	run.assertRunning(200 * time.Millisecond)
}

func TestPageHealthStopsAfterThreeRetries(t *testing.T) {
	logs := &logBuffer{}
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithPageHealth(t, f, logs, dir, "https://chatgpt.com/", []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond})
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")

	sendMainDocumentFailure(t, f, "request-0", "net::ERR_TUNNEL_CONNECTION_FAILED")
	for attempt := 1; attempt <= 3; attempt++ {
		f.awaitIn("session-1", "Page.navigate")
		awaitNavigationStatusMatch(t, dir, 0, func(status navigationStatus) bool {
			return status.PageState == pageStateRetrying && status.PageAttempts == attempt
		})
		sendMainDocumentFailure(t, f, "request-"+string(rune('0'+attempt)), "net::ERR_TUNNEL_CONNECTION_FAILED")
	}

	status := awaitNavigationStatusMatch(t, dir, 0, func(status navigationStatus) bool {
		return status.PageState == pageStateFailed
	})
	assert.Equal(t, 3, status.PageAttempts)
	assert.Equal(t, "ERR_TUNNEL_CONNECTION_FAILED", status.PageError)
	f.drain(50 * time.Millisecond)
	assert.Equal(t, 3, countBufferedMethod(f, "Page.navigate"))
	run.assertRunning(200 * time.Millisecond)
}

func TestReloadOnErrorPageUsesStartURLAndResetsRetryBudget(t *testing.T) {
	logs := &logBuffer{}
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithPageHealth(t, f, logs, dir, "https://chatgpt.com/", []time.Duration{600 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond})
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")

	sendMainDocumentFailure(t, f, "request-0", "net::ERR_TUNNEL_CONNECTION_FAILED")
	awaitNavigationStatusMatch(t, dir, 0, func(status navigationStatus) bool {
		return status.PageState == pageStateRetrying && status.PageAttempts == 0
	})

	require.NoError(t, os.MkdirAll(dir, 0o700))
	writeNavigationCommand(t, dir, navigationCommand{ID: 1, Action: "reload", RequestedAt: 100})
	retry := f.awaitIn("session-1", "Page.navigate")
	params := struct {
		URL string `json:"url"`
	}{}
	decodeParams(t, retry, &params)
	assert.Equal(t, "https://chatgpt.com/", params.URL)

	status := awaitNavigationStatusMatch(t, dir, 1, func(status navigationStatus) bool {
		return status.PageState == pageStateRetrying && status.PageAttempts == 0
	})
	assert.Equal(t, "ERR_TUNNEL_CONNECTION_FAILED", status.PageError)

	sendMainDocumentFailure(t, f, "request-1", "net::ERR_TUNNEL_CONNECTION_FAILED")
	f.awaitInWithin("session-1", "Page.navigate", time.Second)
	status = awaitNavigationStatusMatch(t, dir, 1, func(status navigationStatus) bool {
		return status.PageState == pageStateRetrying && status.PageAttempts == 1
	})
	assert.Equal(t, "ERR_TUNNEL_CONNECTION_FAILED", status.PageError)
	run.assertRunning(200 * time.Millisecond)
}

func TestPageFailureWithEmptyStartURLFailsWithoutRetry(t *testing.T) {
	logs := &logBuffer{}
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithPageHealth(t, f, logs, dir, "", []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond})
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")

	sendMainDocumentFailure(t, f, "request-1", "net::ERR_TUNNEL_CONNECTION_FAILED")
	status := awaitNavigationStatusMatch(t, dir, 0, func(status navigationStatus) bool {
		return status.PageState == pageStateFailed
	})
	assert.Zero(t, status.PageAttempts)
	assert.Equal(t, "ERR_TUNNEL_CONNECTION_FAILED", status.PageError)
	f.drain(50 * time.Millisecond)
	assert.Zero(t, countBufferedMethod(f, "Page.navigate"))
	run.assertRunning(200 * time.Millisecond)
}

func TestPageHealthIgnoresSubframeDocumentFailures(t *testing.T) {
	logs := &logBuffer{}
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithPageHealth(t, f, logs, dir, "https://chatgpt.com/", []time.Duration{10 * time.Millisecond})
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")
	sendMainFrameSuccess(t, f)

	f.send("Network.requestWillBeSent", "session-1", map[string]any{
		"requestId": "frame-request",
		"type":      "Document",
		"frameId":   "frame-child",
		"request":   map[string]any{"url": "https://chatgpt.com/frame"},
	})
	f.send("Network.loadingFailed", "session-1", map[string]any{
		"requestId": "frame-request",
		"type":      "Document",
		"errorText": "net::ERR_FAILED",
	})
	f.drain(50 * time.Millisecond)
	assert.Zero(t, countBufferedMethod(f, "Page.navigate"))
	run.assertRunning(100 * time.Millisecond)
}
