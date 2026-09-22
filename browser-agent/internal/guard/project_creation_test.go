package guard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

// writePermitWithName writes the automatic creation permit the control plane
// issues once the operator has entered a project name.
func writePermitWithName(t *testing.T, dir, permitID, displayName string, ttl time.Duration) {
	t.Helper()
	now := time.Now()
	writeStateFile(t, dir, permitFileName, map[string]any{
		"permit_id":    permitID,
		"kind":         permitKindProjectCreate,
		"issued_at":    now.Unix(),
		"expires_at":   now.Add(ttl).Unix(),
		"display_name": displayName,
	})
}

func writeConsumedPermit(t *testing.T, dir, permitID string) {
	t.Helper()
	writeStateFile(t, dir, permitConsumedFileName, map[string]any{
		"permit_id":   permitID,
		"consumed_at": time.Now().Unix(),
	})
}

func readCreationState(t *testing.T, dir string) (projectCreationFile, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, projectCreationFileName))
	if err != nil {
		return projectCreationFile{}, false
	}
	var file projectCreationFile
	require.NoError(t, json.Unmarshal(data, &file))
	return file, true
}

func awaitCreationState(t *testing.T, dir string, state string) projectCreationFile {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if file, ok := readCreationState(t, dir); ok && file.State == state {
			return file
		}
		if time.Now().After(deadline) {
			file, _ := readCreationState(t, dir)
			t.Fatalf("timed out waiting for creation state %q, last state %+v", state, file)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// probeAnswer is one scripted provider probe response.
type probeAnswer struct {
	found        bool
	loginVisible bool
	disabled     bool
	rect         *probeRect
}

func queueProbe(t *testing.T, f *fakeCDP, answer probeAnswer) {
	t.Helper()
	value := map[string]any{"found": answer.found, "loginVisible": answer.loginVisible, "disabled": answer.disabled}
	if answer.rect != nil {
		value["rect"] = map[string]any{
			"x":      answer.rect.X,
			"y":      answer.rect.Y,
			"width":  answer.rect.Width,
			"height": answer.rect.Height,
		}
	}
	f.queueResult("Runtime.evaluate", map[string]any{
		"result": map[string]any{"type": "object", "value": value},
	})
}

func rectAt(x, y, width, height float64) *probeRect {
	return &probeRect{X: x, Y: y, Width: width, Height: height}
}

func expectMouseClick(t *testing.T, f *fakeCDP, sessionID string, x float64, y float64) {
	t.Helper()
	press := f.awaitIn(sessionID, "Input.dispatchMouseEvent")
	var pressParams struct {
		Type   string  `json:"type"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Button string  `json:"button"`
	}
	decodeParams(t, press, &pressParams)
	assert.Equal(t, "mousePressed", pressParams.Type)
	assert.InDelta(t, x, pressParams.X, 0.01)
	assert.InDelta(t, y, pressParams.Y, 0.01)
	assert.Equal(t, "left", pressParams.Button)

	release := f.awaitIn(sessionID, "Input.dispatchMouseEvent")
	var releaseParams struct {
		Type string `json:"type"`
	}
	decodeParams(t, release, &releaseParams)
	assert.Equal(t, "mouseReleased", releaseParams.Type)
}

func expectTypedText(t *testing.T, f *fakeCDP, sessionID, text string) {
	t.Helper()
	for _, r := range text {
		key := string(r)
		down := f.awaitIn(sessionID, "Input.dispatchKeyEvent")
		var downParams struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}
		decodeParams(t, down, &downParams)
		assert.Equal(t, "keyDown", downParams.Type)
		assert.Equal(t, key, downParams.Key)

		char := f.awaitIn(sessionID, "Input.dispatchKeyEvent")
		var charParams struct {
			Type           string `json:"type"`
			Key            string `json:"key"`
			Text           string `json:"text"`
			UnmodifiedText string `json:"unmodifiedText"`
		}
		decodeParams(t, char, &charParams)
		assert.Equal(t, "char", charParams.Type)
		assert.Equal(t, key, charParams.Key)
		assert.Equal(t, key, charParams.Text)
		assert.Equal(t, key, charParams.UnmodifiedText)

		up := f.awaitIn(sessionID, "Input.dispatchKeyEvent")
		var upParams struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}
		decodeParams(t, up, &upParams)
		assert.Equal(t, "keyUp", upParams.Type)
		assert.Equal(t, key, upParams.Key)
	}
}
func awaitExpressionContaining(t *testing.T, f *fakeCDP, needle string) cdpMessage {
	t.Helper()
	return f.awaitWhere("Runtime.evaluate", 5*time.Second, func(msg cdpMessage) bool {
		if msg.Method != "Runtime.evaluate" {
			return false
		}
		var params struct {
			Expression string `json:"expression"`
		}
		return json.Unmarshal(msg.Params, &params) == nil && strings.Contains(params.Expression, needle)
	})
}

func hasExpressionContaining(f *fakeCDP, needle string) bool {
	for _, msg := range f.buffered {
		if msg.Method != "Runtime.evaluate" {
			continue
		}
		var params struct {
			Expression string `json:"expression"`
		}
		if json.Unmarshal(msg.Params, &params) == nil && strings.Contains(params.Expression, needle) {
			return true
		}
	}
	return false
}

func keyEventCount(f *fakeCDP, key string) int {
	count := 0
	for _, msg := range f.buffered {
		if msg.Method != "Input.dispatchKeyEvent" {
			continue
		}
		var params struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(msg.Params, &params) == nil && params.Key == key {
			count++
		}
	}
	return count
}
func defaultCreationTimings() creationOptions {
	return creationOptions{
		pollInterval: 5 * time.Millisecond,
		timeout:      5 * time.Second,
		uiGrace:      time.Nanosecond,
		stepDelay:    time.Nanosecond,
		submitGrace:  5 * time.Millisecond,
	}
}

// startGuardWithCreation runs the guard with the real page health and creation
// controllers and fast, injected creation timings.
func startGuardWithCreation(t *testing.T, f *fakeCDP, logs *logBuffer, stateDir string, timings creationOptions) *guardRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	run := &guardRun{t: t, cancel: cancel, finished: make(chan struct{})}
	go func() {
		err := Run(ctx, Config{
			CDPURL:              f.server.URL,
			Mode:                policy.ModeLocked,
			Logger:              newTestLogger(logs),
			StateDir:            stateDir,
			StartURL:            "https://chatgpt.com/",
			retryDelays:         []time.Duration{20 * time.Millisecond},
			contentProbeDelays:  []time.Duration{5 * time.Millisecond, 5 * time.Millisecond},
			contentProbeTimeout: 50 * time.Millisecond,
			creationTimings:     timings,
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

func prepareCreationGuard(t *testing.T, timings creationOptions) (*fakeCDP, *logBuffer, string, *guardRun) {
	t.Helper()
	logs := &logBuffer{}
	dir := t.TempDir()
	f := newFakeCDP(t)
	run := startGuardWithCreation(t, f, logs, dir, timings)
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Network.enable")
	return f, logs, dir, run
}

func TestProjectCreationDrivesTheProviderUIAndReportsCreated(t *testing.T) {
	f, _, dir, run := prepareCreationGuard(t, defaultCreationTimings())
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(100, 200, 120, 32)})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(400, 300, 240, 32)})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(640, 400, 100, 32)})
	queueProbe(t, f, probeAnswer{})

	expectMouseClick(t, f, "session-1", 160, 216)
	expectMouseClick(t, f, "session-1", 520, 316)

	expectTypedText(t, f, "session-1", "Quarterly Review")
	expectMouseClick(t, f, "session-1", 690, 416)

	// The ownership stage is the only writer of the single-use marker: it is
	// written when the provider really navigated to the new project.
	writeConsumedPermit(t, dir, "permit-create-1")
	status := awaitCreationState(t, dir, projectCreationStateCreated)
	assert.Equal(t, "permit-create-1", status.PermitID)
	assert.Empty(t, status.Error)
	assert.NotZero(t, status.UpdatedAt)
	run.assertRunning(100 * time.Millisecond)
}

func TestProjectCreationWaitsForSubmitControlBeforeEnterFallback(t *testing.T) {
	timings := defaultCreationTimings()
	timings.pollInterval = 2 * time.Millisecond
	timings.submitGrace = 50 * time.Millisecond
	f, _, dir, run := prepareCreationGuard(t, timings)
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(100, 200, 120, 32)})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(400, 300, 240, 32)})
	queueProbe(t, f, probeAnswer{})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(640, 400, 100, 32)})
	queueProbe(t, f, probeAnswer{})

	expectMouseClick(t, f, "session-1", 160, 216)
	expectMouseClick(t, f, "session-1", 520, 316)
	expectTypedText(t, f, "session-1", "Quarterly Review")
	expectMouseClick(t, f, "session-1", 690, 416)
	assert.False(t, f.hasCommand("session-1", "Input.insertText"))
	assert.Zero(t, keyEventCount(f, "Enter"))

	writeConsumedPermit(t, dir, "permit-create-1")
	status := awaitCreationState(t, dir, projectCreationStateCreated)
	assert.Equal(t, "permit-create-1", status.PermitID)
	assert.Empty(t, status.Error)
	run.assertRunning(100 * time.Millisecond)
}
func TestProjectCreationSynchronizesControlledInputForDisabledSubmit(t *testing.T) {
	timings := defaultCreationTimings()
	timings.pollInterval = 2 * time.Millisecond
	timings.submitGrace = 50 * time.Millisecond
	f, _, dir, run := prepareCreationGuard(t, timings)
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(100, 200, 120, 32)})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(400, 300, 240, 32)})
	queueProbe(t, f, probeAnswer{found: true, disabled: true, rect: rectAt(640, 400, 100, 32)})
	f.queueResult("Runtime.evaluate", map[string]any{
		"result": map[string]any{"type": "object", "value": map[string]any{"found": true}},
	})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(640, 400, 100, 32)})

	expectMouseClick(t, f, "session-1", 160, 216)
	expectMouseClick(t, f, "session-1", 520, 316)
	expectTypedText(t, f, "session-1", "Quarterly Review")
	expectMouseClick(t, f, "session-1", 690, 416)
	assert.True(t, hasExpressionContaining(f, "HTMLInputElement.prototype"))
	assert.True(t, hasExpressionContaining(f, "tracker.setValue"))
	assert.True(t, hasExpressionContaining(f, "new InputEvent('input'"))
	assert.True(t, hasExpressionContaining(f, "type === 'submit'"))
	assert.False(t, f.hasCommand("session-1", "Input.insertText"))
	assert.Zero(t, keyEventCount(f, "Enter"))
	run.assertRunning(100 * time.Millisecond)
}

func TestProjectCreationDoesNotPressEnterForDisabledSubmit(t *testing.T) {
	timings := defaultCreationTimings()
	timings.pollInterval = 2 * time.Millisecond
	timings.submitGrace = 10 * time.Millisecond
	f, _, dir, _ := prepareCreationGuard(t, timings)
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(100, 200, 120, 32)})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(400, 300, 240, 32)})
	queueProbe(t, f, probeAnswer{found: true, disabled: true, rect: rectAt(640, 400, 100, 32)})
	f.queueResult("Runtime.evaluate", map[string]any{
		"result": map[string]any{"type": "object", "value": map[string]any{"found": true}},
	})
	for i := 0; i < 100; i++ {
		queueProbe(t, f, probeAnswer{found: true, disabled: true, rect: rectAt(640, 400, 100, 32)})
	}

	awaitExpressionContaining(t, f, "HTMLInputElement.prototype")
	status := awaitCreationState(t, dir, projectCreationStateFailed)
	assert.Equal(t, creationErrorNameRejected, status.Error)
	assert.True(t, hasExpressionContaining(f, "HTMLInputElement.prototype"))
	assert.Zero(t, keyEventCount(f, "Enter"))
	assert.NoFileExists(t, filepath.Join(dir, permitConsumedFileName))
}

func TestProjectCreationFallsBackToEnterAfterSubmitGrace(t *testing.T) {
	timings := defaultCreationTimings()
	timings.pollInterval = 2 * time.Millisecond
	timings.submitGrace = 10 * time.Millisecond
	f, _, dir, run := prepareCreationGuard(t, timings)
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(100, 200, 120, 32)})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(400, 300, 240, 32)})

	expectMouseClick(t, f, "session-1", 160, 216)
	expectMouseClick(t, f, "session-1", 520, 316)
	expectTypedText(t, f, "session-1", "Quarterly Review")

	down := f.awaitIn("session-1", "Input.dispatchKeyEvent")
	var downParams struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	decodeParams(t, down, &downParams)
	assert.Equal(t, "keyDown", downParams.Type)
	assert.Equal(t, "Enter", downParams.Key)

	up := f.awaitIn("session-1", "Input.dispatchKeyEvent")
	var upParams struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	decodeParams(t, up, &upParams)
	assert.Equal(t, "keyUp", upParams.Type)
	assert.Equal(t, "Enter", upParams.Key)

	writeConsumedPermit(t, dir, "permit-create-1")
	status := awaitCreationState(t, dir, projectCreationStateCreated)
	assert.Equal(t, "permit-create-1", status.PermitID)
	assert.Empty(t, status.Error)
	run.assertRunning(100 * time.Millisecond)
}
func TestProjectCreationFailsClosedWhenTheProviderIsSignedOut(t *testing.T) {
	f, _, dir, run := prepareCreationGuard(t, defaultCreationTimings())
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{loginVisible: true})
	queueProbe(t, f, probeAnswer{loginVisible: true})

	status := awaitCreationState(t, dir, projectCreationStateFailed)
	assert.Equal(t, creationErrorLoginRequired, status.Error)
	// A failed attempt must not consume the permit: the operator can still
	// finish the creation manually in the revealed window.
	assert.NoFileExists(t, filepath.Join(dir, permitConsumedFileName))
	assert.FileExists(t, filepath.Join(dir, permitFileName))

	// The manual completion the ownership stage allows is still observed.
	writeConsumedPermit(t, dir, "permit-create-1")
	created := awaitCreationState(t, dir, projectCreationStateCreated)
	assert.Equal(t, "permit-create-1", created.PermitID)
	assert.Empty(t, created.Error)
	run.assertRunning(100 * time.Millisecond)
}

func TestProjectCreationReportsUINotFoundWhenNoLoginIsVisible(t *testing.T) {
	f, _, dir, _ := prepareCreationGuard(t, defaultCreationTimings())
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{})
	queueProbe(t, f, probeAnswer{})

	status := awaitCreationState(t, dir, projectCreationStateFailed)
	assert.Equal(t, creationErrorUINotFound, status.Error)
	assert.NoFileExists(t, filepath.Join(dir, permitConsumedFileName))
}

func TestProjectCreationReportsNameRejected(t *testing.T) {
	f, _, dir, _ := prepareCreationGuard(t, defaultCreationTimings())
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(10, 10, 100, 30)})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(20, 20, 200, 30)})
	queueProbe(t, f, probeAnswer{found: true, rect: rectAt(30, 30, 100, 30)})
	queueProbe(t, f, probeAnswer{found: true})

	status := awaitCreationState(t, dir, projectCreationStateFailed)
	assert.Equal(t, creationErrorNameRejected, status.Error)
	assert.NoFileExists(t, filepath.Join(dir, permitConsumedFileName))
}

func TestProjectCreationTimesOutAndLeavesThePermitUsable(t *testing.T) {
	timings := defaultCreationTimings()
	timings.timeout = 60 * time.Millisecond
	timings.uiGrace = time.Second
	f, _, dir, _ := prepareCreationGuard(t, timings)
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{})
	queueProbe(t, f, probeAnswer{})
	queueProbe(t, f, probeAnswer{})

	status := awaitCreationState(t, dir, projectCreationStateFailed)
	assert.Equal(t, creationErrorTimeout, status.Error)
	assert.NoFileExists(t, filepath.Join(dir, permitConsumedFileName))
}

func TestProjectCreationIgnoresLegacyPermitsWithoutAName(t *testing.T) {
	f, _, dir, run := prepareCreationGuard(t, defaultCreationTimings())
	writePermit(t, dir, "permit-manual-1", 5*time.Minute)

	sendMainFrameReady(t, f)
	f.drain(300 * time.Millisecond)

	assert.NoFileExists(t, filepath.Join(dir, projectCreationFileName))
	assert.False(t, f.hasCommand("session-1", "Input.insertText"))
	assert.False(t, f.hasCommand("session-1", "Input.dispatchMouseEvent"))
	run.assertRunning(50 * time.Millisecond)
}

func TestProjectCreationDoesNotRestartAFinishedPermit(t *testing.T) {
	f, _, dir, _ := prepareCreationGuard(t, defaultCreationTimings())
	writePermitWithName(t, dir, "permit-create-1", "Quarterly Review", 5*time.Minute)

	sendMainFrameReady(t, f)
	queueProbe(t, f, probeAnswer{loginVisible: true})
	queueProbe(t, f, probeAnswer{loginVisible: true})

	status := awaitCreationState(t, dir, projectCreationStateFailed)
	assert.Equal(t, creationErrorLoginRequired, status.Error)
	assert.EqualValues(t, status.UpdatedAt, readCreationStateFile(t, dir).UpdatedAt)

	// The failed permit is not retried on its own: a retry needs a new permit.
	f.drain(200 * time.Millisecond)
	assert.False(t, f.hasCommand("session-1", "Input.insertText"))
	assert.Equal(t, creationErrorLoginRequired, readCreationStateFile(t, dir).Error)
}

func readCreationStateFile(t *testing.T, dir string) projectCreationFile {
	t.Helper()
	file, ok := readCreationState(t, dir)
	require.True(t, ok, "project-creation.json must exist")
	return file
}
