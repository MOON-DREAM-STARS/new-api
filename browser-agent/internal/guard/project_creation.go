package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

// Project creation controller (phase 4b).
//
// The operator never has to touch the cropped provider sidebar: the control
// plane issues a creation permit that carries the operator-facing project name,
// and this controller performs the real provider interaction through the single
// existing CDP consumer. It never invents a project, never records an
// observation and never consumes the permit: the ownership stage still decides
// whether the navigation the provider really performed is allowed, and it is
// the ownership stage that consumes the permit and writes project_created.
//
// Every step is expressed with role/accessible-name probes instead of provider
// class names, and every failure fails closed into the stable ERR_* codes so the
// workspace can reveal the real provider window and let the operator finish.
const (
	projectCreationFileName = "project-creation.json"

	// projectCreationPollInterval matches the order of the navigation command
	// poll so a freshly issued permit is picked up quickly.
	projectCreationPollInterval = 300 * time.Millisecond

	// projectCreationTimeout bounds one automatic attempt. It stays well below
	// the permit TTL, so a failed attempt always leaves time for the operator to
	// finish the creation manually in the revealed window.
	projectCreationTimeout = 90 * time.Second

	// projectCreationUIGrace is how long the controller keeps probing for the
	// provider creation entry before it declares the entry missing. A provider
	// page that is READY but still hydrating is not a failure yet.
	projectCreationUIGrace = 15 * time.Second

	// projectCreationSubmitGrace is the minimum wait after submitting the name
	// before a visible validation error is treated as a rejection.
	projectCreationSubmitGrace = 2 * time.Second

	// projectCreationStepDelay spaces the UI steps so the provider page can
	// settle between a click and the next probe.
	projectCreationStepDelay = 400 * time.Millisecond

	projectCreationStateRunning = "RUNNING"
	projectCreationStateCreated = "CREATED"
	projectCreationStateFailed  = "FAILED"

	creationErrorLoginRequired = "ERR_PROVIDER_LOGIN_REQUIRED"
	creationErrorUINotFound    = "ERR_PROJECT_UI_NOT_FOUND"
	creationErrorNameRejected  = "ERR_PROJECT_NAME_REJECTED"
	creationErrorTimeout       = "ERR_PROJECT_CREATE_TIMEOUT"
)

// projectCreationFile is the on-disk project-creation.json shape. The agent only
// reads it; the guard owns every write.
type projectCreationFile struct {
	PermitID  string `json:"permit_id"`
	State     string `json:"state"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

type creationPhase uint8

const (
	creationPhaseNone creationPhase = iota
	creationPhaseOpenUI
	creationPhaseAwaitInput
	creationPhaseAwaitSubmit
	creationPhaseAwaitResult
)

// probeRect is the viewport rectangle of one provider element, in remote
// pixels. It is the only geometry that leaves the page.
type probeRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (r *probeRect) center() (float64, float64, bool) {
	if r == nil || r.Width <= 0 || r.Height <= 0 {
		return 0, 0, false
	}
	return r.X + r.Width/2, r.Y + r.Height/2, true
}

// creationProbe is the JSON result of one provider probe. Nothing else from the
// provider document is read, stored or logged.
type creationProbe struct {
	Found        bool       `json:"found"`
	LoginVisible bool       `json:"loginVisible"`
	Rect         *probeRect `json:"rect"`
}

// creationOptions overrides the frozen creation timings. Zero values select the
// production constants; only tests set these.
type creationOptions struct {
	pollInterval time.Duration
	timeout      time.Duration
	uiGrace      time.Duration
	stepDelay    time.Duration
	submitGrace  time.Duration
}

// projectCreationController drives one automatic provider creation at a time.
// It shares the existing CDP client, so it never opens a second connection or a
// second event consumer.
type projectCreationController struct {
	dir          string
	mode         policy.Mode
	client       *cdpClient
	navigation   *navigationController
	pageHealth   *pageHealthController
	logger       *slog.Logger
	now          func() time.Time
	pollInterval time.Duration
	timeout      time.Duration
	uiGrace      time.Duration
	stepDelay    time.Duration
	submitGrace  time.Duration

	mu        sync.Mutex
	permitID  string
	state     string
	errorCode string
	updatedAt int64
	phase     creationPhase
	phaseAt   time.Time
	deadline  time.Time
}

func newProjectCreationController(dir string, mode policy.Mode, client *cdpClient, navigation *navigationController, pageHealth *pageHealthController, logger *slog.Logger, options creationOptions) *projectCreationController {
	if logger == nil {
		logger = slog.Default()
	}
	controller := &projectCreationController{
		dir:          strings.TrimSpace(dir),
		mode:         mode,
		client:       client,
		navigation:   navigation,
		pageHealth:   pageHealth,
		logger:       logger,
		now:          time.Now,
		pollInterval: options.pollInterval,
		timeout:      options.timeout,
		uiGrace:      options.uiGrace,
		stepDelay:    options.stepDelay,
		submitGrace:  options.submitGrace,
	}
	if controller.pollInterval <= 0 {
		controller.pollInterval = projectCreationPollInterval
	}
	if controller.timeout <= 0 {
		controller.timeout = projectCreationTimeout
	}
	if controller.uiGrace <= 0 {
		controller.uiGrace = projectCreationUIGrace
	}
	if controller.stepDelay <= 0 {
		controller.stepDelay = projectCreationStepDelay
	}
	if controller.submitGrace <= 0 {
		controller.submitGrace = projectCreationSubmitGrace
	}
	controller.load()
	return controller
}

// load seeds the in-memory state from the last written state file so a guard
// restart does not start a second attempt for a permit it already finished.
func (c *projectCreationController) load() {
	if c == nil || c.dir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(c.dir, projectCreationFileName))
	if err != nil {
		return
	}
	var file projectCreationFile
	if err := json.Unmarshal(data, &file); err != nil {
		return
	}
	if !validCreationState(file.State) {
		return
	}
	c.mu.Lock()
	c.permitID = file.PermitID
	c.state = file.State
	c.errorCode = file.Error
	c.updatedAt = file.UpdatedAt
	c.mu.Unlock()
}

func validCreationState(state string) bool {
	switch state {
	case projectCreationStateRunning, projectCreationStateCreated, projectCreationStateFailed:
		return true
	default:
		return false
	}
}

// run polls the permit file until the guard stops. A step error is logged and
// the loop continues: a provider page that is temporarily unreachable must not
// stop enforcement.
func (c *projectCreationController) run(ctx context.Context) {
	if c == nil || c.client == nil || c.navigation == nil {
		return
	}
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.process(ctx); err != nil {
				c.logger.Debug("project creation step failed",
					"event", "project_creation_step_failed",
					"component", "guard",
					"mode", string(c.mode),
					"error", err.Error(),
				)
			}
		}
	}
}

func (c *projectCreationController) process(ctx context.Context) error {
	permit, ok := c.detectPermit()
	if !ok {
		return nil
	}
	if running := c.stateOf() == projectCreationStateRunning && c.permitIDOf() != permit.PermitID; running {
		// A different attempt is still running: starting this permit now would
		// open a second provider creation.
		if !c.timedOut() {
			return nil
		}
		// The abandoned attempt timed out. The failure belongs to that permit,
		// and the new permit can start on the next poll.
		return c.fail(c.permitIDOf(), creationErrorTimeout)
	}
	// A consumed permit is the provider fact: the ownership stage only writes the
	// marker after the real navigation to the new project. It wins over a recorded
	// failure, because the operator may have finished the creation manually in
	// the revealed window.
	if permitConsumedByDir(c.dir, permit.PermitID) {
		if c.stateOf() == projectCreationStateCreated && c.permitIDOf() == permit.PermitID {
			return nil
		}
		return c.markCreated(permit.PermitID)
	}
	// A recorded failure of this permit is not retried on its own: a retry needs
	// a fresh permit from the control plane.
	if c.finishedWith(permit.PermitID) {
		return nil
	}
	sessionID := c.navigation.currentSession()
	if sessionID == "" {
		// The page target is not attached yet; the deadline still applies.
		return nil
	}
	if !c.runningWith(permit.PermitID) {
		if err := c.begin(permit); err != nil {
			return err
		}
	}
	if c.timedOut() {
		return c.fail(permit.PermitID, creationErrorTimeout)
	}
	return c.advance(ctx, sessionID, permit)
}

// detectPermit reports the permit that asks for an automatic creation. It stays
// silent for the legacy manual flow, which carries no display name.
func (c *projectCreationController) detectPermit() (permitFile, bool) {
	if c == nil || c.dir == "" {
		return permitFile{}, false
	}
	data, err := os.ReadFile(filepath.Join(c.dir, permitFileName))
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
	if strings.TrimSpace(permit.DisplayName) == "" {
		return permitFile{}, false
	}
	if permit.ExpiresAt <= c.now().Unix() {
		return permitFile{}, false
	}
	return permit, true
}

func (c *projectCreationController) advance(ctx context.Context, sessionID string, permit permitFile) error {
	if c.pageFailed() {
		return c.fail(permit.PermitID, creationErrorUINotFound)
	}
	if !c.pageReady() {
		return nil
	}
	switch c.phaseOf() {
	case creationPhaseOpenUI:
		return c.stepOpenUI(ctx, sessionID, permit)
	case creationPhaseAwaitInput:
		return c.stepAwaitInput(ctx, sessionID, permit)
	case creationPhaseAwaitSubmit:
		return c.stepAwaitSubmit(ctx, sessionID, permit)
	case creationPhaseAwaitResult:
		return c.stepAwaitResult(ctx, sessionID, permit)
	default:
		return nil
	}
}

func (c *projectCreationController) stepOpenUI(ctx context.Context, sessionID string, permit permitFile) error {
	probe, err := c.probe(ctx, sessionID, probeNewProjectExpression)
	if err != nil {
		return err
	}
	if !probe.Found {
		if c.now().Sub(c.phaseStart()) < c.uiGrace {
			return nil
		}
		if probe.LoginVisible {
			return c.fail(permit.PermitID, creationErrorLoginRequired)
		}
		return c.fail(permit.PermitID, creationErrorUINotFound)
	}
	x, y, ok := probe.Rect.center()
	if !ok {
		return nil
	}
	if err := c.click(ctx, sessionID, x, y); err != nil {
		return err
	}
	c.setPhase(creationPhaseAwaitInput)
	return nil
}

func (c *projectCreationController) stepAwaitInput(ctx context.Context, sessionID string, permit permitFile) error {
	probe, err := c.probe(ctx, sessionID, probeProjectNameExpression)
	if err != nil {
		return err
	}
	if !probe.Found {
		// The creation form may still be animating in; the overall deadline
		// bounds how long this waits.
		return nil
	}
	x, y, ok := probe.Rect.center()
	if !ok {
		return nil
	}
	if err := c.click(ctx, sessionID, x, y); err != nil {
		return err
	}
	if err := c.client.call(ctx, sessionID, "Input.insertText", map[string]any{
		"text": strings.TrimSpace(permit.DisplayName),
	}); err != nil {
		return err
	}
	c.setPhase(creationPhaseAwaitSubmit)
	return nil
}

func (c *projectCreationController) stepAwaitSubmit(ctx context.Context, sessionID string, permit permitFile) error {
	probe, err := c.probe(ctx, sessionID, probeSubmitExpression)
	if err != nil {
		return err
	}
	if probe.Found {
		if x, y, ok := probe.Rect.center(); ok {
			if err := c.click(ctx, sessionID, x, y); err != nil {
				return err
			}
			c.setPhase(creationPhaseAwaitResult)
			return nil
		}
	}
	// No explicit submit control: the name field is focused, so Enter submits.
	if err := c.pressEnter(ctx, sessionID); err != nil {
		return err
	}
	c.setPhase(creationPhaseAwaitResult)
	return nil
}

func (c *projectCreationController) stepAwaitResult(ctx context.Context, sessionID string, permit permitFile) error {
	if c.now().Sub(c.phaseStart()) < c.submitGrace {
		return nil
	}
	rejected, err := c.probe(ctx, sessionID, probeNameRejectedExpression)
	if err != nil {
		return err
	}
	if rejected.Found {
		return c.fail(permit.PermitID, creationErrorNameRejected)
	}
	return nil
}

// probe evaluates one provider probe and decodes its value. A probe that cannot
// be evaluated is returned as an error and never as a negative answer.
func (c *projectCreationController) probe(ctx context.Context, sessionID string, expression string) (creationProbe, error) {
	result, err := c.client.callResult(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
	})
	if err != nil {
		return creationProbe{}, err
	}
	var evaluated struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &evaluated); err != nil {
		return creationProbe{}, fmt.Errorf("parse Runtime.evaluate: %w", err)
	}
	if len(evaluated.ExceptionDetails) > 0 && string(evaluated.ExceptionDetails) != "null" {
		return creationProbe{}, errors.New("provider probe raised an exception")
	}
	if len(evaluated.Result.Value) == 0 {
		return creationProbe{}, nil
	}
	var probe creationProbe
	if err := json.Unmarshal(evaluated.Result.Value, &probe); err != nil {
		return creationProbe{}, fmt.Errorf("parse provider probe: %w", err)
	}
	return probe, nil
}

// click dispatches a real mouse click at remote framebuffer coordinates, so the
// provider page reacts exactly as it does to operator input.
func (c *projectCreationController) click(ctx context.Context, sessionID string, x float64, y float64) error {
	base := map[string]any{
		"x":          x,
		"y":          y,
		"button":     "left",
		"clickCount": 1,
	}
	pressed := map[string]any{"type": "mousePressed"}
	for key, value := range base {
		pressed[key] = value
	}
	if err := c.client.call(ctx, sessionID, "Input.dispatchMouseEvent", pressed); err != nil {
		return err
	}
	released := map[string]any{"type": "mouseReleased"}
	for key, value := range base {
		released[key] = value
	}
	if err := c.client.call(ctx, sessionID, "Input.dispatchMouseEvent", released); err != nil {
		return err
	}
	return c.settle(ctx)
}

func (c *projectCreationController) pressEnter(ctx context.Context, sessionID string) error {
	key := map[string]any{
		"key":                   "Enter",
		"code":                  "Enter",
		"windowsVirtualKeyCode": 13,
		"nativeVirtualKeyCode":  13,
	}
	down := map[string]any{"type": "keyDown"}
	for k, v := range key {
		down[k] = v
	}
	if err := c.client.call(ctx, sessionID, "Input.dispatchKeyEvent", down); err != nil {
		return err
	}
	up := map[string]any{"type": "keyUp"}
	for k, v := range key {
		up[k] = v
	}
	if err := c.client.call(ctx, sessionID, "Input.dispatchKeyEvent", up); err != nil {
		return err
	}
	return c.settle(ctx)
}

// settle waits the frozen step delay, bounded by the caller's context.
func (c *projectCreationController) settle(ctx context.Context) error {
	delay := c.stepDelay
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *projectCreationController) begin(permit permitFile) error {
	now := c.now()
	c.mu.Lock()
	c.permitID = permit.PermitID
	c.state = projectCreationStateRunning
	c.errorCode = ""
	c.phase = creationPhaseOpenUI
	c.phaseAt = now
	c.deadline = now.Add(c.timeout)
	c.mu.Unlock()
	c.audit("project_creation_started", permit.PermitID)
	return c.persist(projectCreationStateRunning, "")
}

func (c *projectCreationController) markCreated(permitID string) error {
	c.mu.Lock()
	c.permitID = permitID
	c.state = projectCreationStateCreated
	c.errorCode = ""
	c.phase = creationPhaseNone
	c.mu.Unlock()
	c.audit("project_creation_created", permitID)
	return c.persist(projectCreationStateCreated, "")
}

// fail records a stable failure without consuming the permit, so the operator
// can either retry with a fresh permit or finish the creation manually.
func (c *projectCreationController) fail(permitID string, errorCode string) error {
	c.mu.Lock()
	c.permitID = permitID
	c.state = projectCreationStateFailed
	c.errorCode = errorCode
	c.phase = creationPhaseNone
	c.mu.Unlock()
	c.audit("project_creation_failed", permitID, errorCode)
	return c.persist(projectCreationStateFailed, errorCode)
}

func (c *projectCreationController) persist(state string, errorCode string) error {
	if c == nil || c.dir == "" {
		return nil
	}
	c.mu.Lock()
	permitID := c.permitID
	c.updatedAt = c.now().Unix()
	file := projectCreationFile{
		PermitID:  permitID,
		State:     state,
		Error:     errorCode,
		UpdatedAt: c.updatedAt,
	}
	c.mu.Unlock()
	payload, err := json.Marshal(file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	return writeNavigationStatusAtomic(filepath.Join(c.dir, projectCreationFileName), payload)
}

func (c *projectCreationController) audit(event string, permitID string, errorCode ...string) {
	fields := []any{
		"event", event,
		"component", "guard",
		"mode", string(c.mode),
	}
	if len(errorCode) > 0 && errorCode[0] != "" {
		fields = append(fields, "reason", errorCode[0])
	}
	c.logger.Info("project creation event", fields...)
}

func (c *projectCreationController) stateOf() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *projectCreationController) permitIDOf() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.permitID
}

func (c *projectCreationController) phaseOf() creationPhase {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase
}

func (c *projectCreationController) phaseStart() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phaseAt
}

func (c *projectCreationController) setPhase(phase creationPhase) {
	c.mu.Lock()
	c.phase = phase
	c.phaseAt = c.now()
	c.mu.Unlock()
}

func (c *projectCreationController) timedOut() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.deadline.IsZero() && !c.now().Before(c.deadline)
}

func (c *projectCreationController) runningWith(permitID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state == projectCreationStateRunning && c.permitID == permitID
}

func (c *projectCreationController) finishedWith(permitID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.permitID != permitID {
		return false
	}
	return c.state == projectCreationStateCreated || c.state == projectCreationStateFailed
}

func (c *projectCreationController) pageReady() bool {
	if c.pageHealth == nil {
		return false
	}
	return c.pageHealth.snapshot().state == pageStateReady
}

func (c *projectCreationController) pageFailed() bool {
	if c.pageHealth == nil {
		return false
	}
	return c.pageHealth.snapshot().state == pageStateFailed
}

// permitConsumedByDir reports whether the guard already recorded the single use
// of permitID. It fails closed: a marker that exists but cannot be read or
// parsed counts as consumed.
func permitConsumedByDir(dir string, permitID string) bool {
	data, err := os.ReadFile(filepath.Join(dir, permitConsumedFileName))
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	var consumed consumedFile
	if err := json.Unmarshal(data, &consumed); err != nil {
		return true
	}
	return consumed.PermitID == permitID
}

// probeNewProjectExpression looks for the provider creation entry by role and
// accessible name. It never reads a provider class name, and it reports whether
// a sign-in control is visible so a missing entry can be told apart from a
// signed-out shell.
const probeNewProjectExpression = `(() => {
	const norm = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const nameOf = (el) => {
		const label = norm(el.getAttribute('aria-label'));
		if (label) return label;
		const title = norm(el.getAttribute('title'));
		if (title) return title;
		return norm(el.innerText);
	};
	const rectOf = (el) => {
		const rect = el.getBoundingClientRect();
		if (rect.width <= 0 || rect.height <= 0) return null;
		return rect;
	};
	const nodes = Array.from(document.querySelectorAll('button, a, [role="button"], [role="menuitem"], [role="link"]'));
	const loginVisible = nodes.some((el) => {
		if (!rectOf(el)) return false;
		return /^(log in|sign in|sign up|create account)\b/.test(nameOf(el));
	});
	for (const el of nodes) {
		if (!/^new project\b/.test(nameOf(el))) continue;
		const rect = rectOf(el);
		if (!rect) continue;
		return { found: true, loginVisible: loginVisible, rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height } };
	}
	return { found: false, loginVisible: loginVisible };
})()`

// probeProjectNameExpression locates the creation name field. Only a field whose
// accessible name or placeholder mentions the project name is accepted, so the
// controller never types the project name into an unrelated composer.
const probeProjectNameExpression = `(() => {
	const rectOf = (el) => {
		const rect = el.getBoundingClientRect();
		if (rect.width <= 0 || rect.height <= 0) return null;
		return rect;
	};
	const fields = Array.from(document.querySelectorAll('input, textarea, [contenteditable="true"], [role="textbox"]'));
	for (const el of fields) {
		const rect = rectOf(el);
		if (!rect) continue;
		const label = ((el.getAttribute('aria-label') || '') + ' ' + (el.getAttribute('placeholder') || '') + ' ' + (el.getAttribute('name') || '')).toLowerCase();
		if (!/project|name/.test(label)) continue;
		return { found: true, rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height } };
	}
	return { found: false };
})()`

// probeSubmitExpression locates the explicit submit control of the creation
// form, if the provider offers one instead of submitting on Enter.
const probeSubmitExpression = `(() => {
	const norm = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const rectOf = (el) => {
		const rect = el.getBoundingClientRect();
		if (rect.width <= 0 || rect.height <= 0) return null;
		return rect;
	};
	const scope = document.querySelector('[role="dialog"]') || document;
	const nodes = Array.from(scope.querySelectorAll('button, [role="button"]'));
	for (const el of nodes) {
		const name = norm(el.getAttribute('aria-label')) || norm(el.innerText);
		if (!/^(create|create project|save|save project|add|next|continue|done)\b/.test(name)) continue;
		const rect = rectOf(el);
		if (!rect) continue;
		return { found: true, rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height } };
	}
	return { found: false };
})()`

// probeNameRejectedExpression reports a visible validation error in the creation
// form, which means the provider refused the name instead of creating.
const probeNameRejectedExpression = `(() => {
	const visible = (el) => {
		const rect = el.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const invalid = Array.from(document.querySelectorAll('[aria-invalid="true"]')).some(visible);
	if (invalid) return { found: true };
	const alerts = Array.from(document.querySelectorAll('[role="alert"]'));
	for (const el of alerts) {
		if (!visible(el)) continue;
		const text = (el.innerText || '').trim();
		if (text.length > 0 && text.length <= 300) return { found: true };
	}
	return { found: false };
})()`
