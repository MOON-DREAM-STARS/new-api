package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	inputStateFileName   = "input.json"
	inputCommandFileName = "input-command.json"
	inputPollInterval    = 50 * time.Millisecond
	inputTextMaxRunes    = 4096
)

const (
	inputActionText  = "input_text"
	inputActionKey   = "input_key"
	inputActionCaret = "caret_probe"

	inputStateDone   = "DONE"
	inputStateFailed = "FAILED"

	inputErrorUnavailable = "WEB_WORKSPACE_INPUT_UNAVAILABLE"
	inputErrorTimeout     = "WEB_WORKSPACE_INPUT_TIMEOUT"
	inputErrorRejected    = "WEB_WORKSPACE_INPUT_REJECTED"
)

// inputCaret is the remote browser caret rectangle in CSS pixels relative to the
// remote viewport. It is deliberately not a DOM node or CDP identifier.
type inputCaret struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type inputCommand struct {
	ID          int64    `json:"id"`
	Action      string   `json:"action"`
	Text        string   `json:"text,omitempty"`
	Key         string   `json:"key,omitempty"`
	Modifiers   []string `json:"modifiers,omitempty"`
	RequestedAt int64    `json:"requested_at"`
}

type inputState struct {
	ID        int64       `json:"id"`
	Action    string      `json:"action"`
	State     string      `json:"state"`
	Error     string      `json:"error"`
	Caret     *inputCaret `json:"caret"`
	UpdatedAt int64       `json:"updated_at"`
}

type inputStateFile struct {
	ID        *int64      `json:"id"`
	Action    *string     `json:"action"`
	State     *string     `json:"state"`
	Error     *string     `json:"error"`
	Caret     *inputCaret `json:"caret"`
	UpdatedAt *int64      `json:"updated_at"`
}

func (file inputStateFile) valid() bool {
	if file.ID == nil || *file.ID < 0 || file.Action == nil || file.State == nil ||
		file.Error == nil || file.UpdatedAt == nil {
		return false
	}
	if *file.Action != inputActionText && *file.Action != inputActionKey && *file.Action != inputActionCaret {
		return false
	}
	if *file.State != inputStateDone && *file.State != inputStateFailed {
		return false
	}
	if *file.UpdatedAt < 0 {
		return false
	}
	if *file.State == inputStateFailed && !validInputError(*file.Error) {
		return false
	}
	if file.Caret != nil && !validInputCaret(*file.Caret) {
		return false
	}
	return true
}

func (file inputStateFile) value() inputState {
	return inputState{
		ID:        *file.ID,
		Action:    *file.Action,
		State:     *file.State,
		Error:     *file.Error,
		Caret:     file.Caret,
		UpdatedAt: *file.UpdatedAt,
	}
}

func validInputError(value string) bool {
	switch value {
	case inputErrorUnavailable, inputErrorTimeout, inputErrorRejected:
		return true
	default:
		return false
	}
}

func validInputCaret(caret inputCaret) bool {
	return caret.X >= 0 && caret.Y >= 0 && caret.Width >= 0 && caret.Height >= 0 &&
		caret.X < 100000 && caret.Y < 100000 && caret.Width < 100000 && caret.Height < 100000
}

func validInputKey(key string) bool {
	switch key {
	case "Enter", "Backspace", "Delete", "Escape", "Tab", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
		"Home", "End", "PageUp", "PageDown", "a", "A", "c", "C", "v", "V":
		return true
	default:
		return false
	}
}

func validInputModifier(modifier string) bool {
	switch modifier {
	case "ctrl", "shift", "alt", "meta":
		return true
	default:
		return false
	}
}

func validInputCommand(command inputCommand) bool {
	if command.ID <= 0 || command.RequestedAt < 0 {
		return false
	}
	switch command.Action {
	case inputActionText:
		return strings.TrimSpace(command.Text) != "" && utf8.RuneCountInString(command.Text) <= inputTextMaxRunes
	case inputActionKey:
		if !validInputKey(strings.TrimSpace(command.Key)) || len(command.Modifiers) > 4 {
			return false
		}
		seen := map[string]struct{}{}
		for _, modifier := range command.Modifiers {
			modifier = strings.ToLower(strings.TrimSpace(modifier))
			if !validInputModifier(modifier) {
				return false
			}
			if _, ok := seen[modifier]; ok {
				return false
			}
			seen[modifier] = struct{}{}
		}
		return true
	case inputActionCaret:
		return command.Text == "" && command.Key == "" && len(command.Modifiers) == 0
	default:
		return false
	}
}

type inputController struct {
	dir        string
	client     *cdpClient
	navigation *navigationController
	logger     *slog.Logger
	now        func() time.Time

	stateMu     sync.Mutex
	lastSeen    int64
	lastWritten int64
}

func newInputController(dir string, client *cdpClient, navigation *navigationController, logger *slog.Logger) *inputController {
	if logger == nil {
		logger = slog.Default()
	}
	controller := &inputController{
		dir:        strings.TrimSpace(dir),
		client:     client,
		navigation: navigation,
		logger:     logger,
		now:        time.Now,
	}
	if data, err := os.ReadFile(controller.filePath(inputStateFileName)); err == nil {
		var file inputStateFile
		if json.Unmarshal(data, &file) == nil && file.valid() {
			controller.lastSeen = *file.ID
			controller.lastWritten = *file.ID
		}
	}
	return controller
}

func (c *inputController) filePath(name string) string {
	if c == nil || c.dir == "" {
		return ""
	}
	return filepath.Join(c.dir, name)
}

func (c *inputController) run(ctx context.Context) {
	ticker := time.NewTicker(inputPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.processCommand(ctx); err != nil {
				c.logger.Debug("input command failed", "event", "input_command_failed", "component", "guard", "error", err)
			}
		}
	}
}

func (c *inputController) processCommand(ctx context.Context) error {
	if c == nil || c.client == nil {
		return nil
	}
	data, err := os.ReadFile(c.filePath(inputCommandFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var command inputCommand
	if err := json.Unmarshal(data, &command); err != nil || !validInputCommand(command) {
		return nil
	}

	c.stateMu.Lock()
	if command.ID <= c.lastSeen {
		c.stateMu.Unlock()
		return nil
	}
	c.lastSeen = command.ID
	c.stateMu.Unlock()

	if err := c.applyCommand(ctx, command); err != nil {
		c.logger.Debug("input command apply failed", "event", "input_command_apply_failed", "component", "guard", "action", command.Action, "error", err)
		return c.writeState(inputState{
			ID:        command.ID,
			Action:    command.Action,
			State:     inputStateFailed,
			Error:     inputErrorUnavailable,
			UpdatedAt: c.now().Unix(),
		})
	}
	return nil
}

func (c *inputController) applyCommand(ctx context.Context, command inputCommand) error {
	sessionID := ""
	if c.navigation != nil {
		sessionID = c.navigation.currentSession()
	}
	if sessionID == "" {
		return errors.New("input page session unavailable")
	}

	switch command.Action {
	case inputActionText:
		if err := c.client.call(ctx, sessionID, "Input.insertText", map[string]any{"text": command.Text}); err != nil {
			return err
		}
	case inputActionKey:
		if err := c.dispatchKey(ctx, sessionID, command.Key, command.Modifiers); err != nil {
			return err
		}
	case inputActionCaret:
		caret, err := c.probeCaret(ctx, sessionID)
		if err != nil {
			return err
		}
		return c.writeState(inputState{
			ID:        command.ID,
			Action:    command.Action,
			State:     inputStateDone,
			Caret:     caret,
			UpdatedAt: c.now().Unix(),
		})
	default:
		return errors.New("unsupported input action")
	}
	return c.writeState(inputState{
		ID:        command.ID,
		Action:    command.Action,
		State:     inputStateDone,
		UpdatedAt: c.now().Unix(),
	})
}

func (c *inputController) dispatchKey(ctx context.Context, sessionID, key string, modifiers []string) error {
	key = strings.TrimSpace(key)
	if key == "Esc" {
		key = "Escape"
	}
	if key == "A" {
		key = "a"
	}
	if key == "C" {
		key = "c"
	}
	if key == "V" {
		key = "v"
	}
	specs := map[string]struct {
		key  string
		code string
		vk   int
	}{
		"Enter":      {"Enter", "Enter", 13},
		"Backspace":  {"Backspace", "Backspace", 8},
		"Delete":     {"Delete", "Delete", 46},
		"Escape":     {"Escape", "Escape", 27},
		"Tab":        {"Tab", "Tab", 9},
		"ArrowUp":    {"ArrowUp", "ArrowUp", 38},
		"ArrowDown":  {"ArrowDown", "ArrowDown", 40},
		"ArrowLeft":  {"ArrowLeft", "ArrowLeft", 37},
		"ArrowRight": {"ArrowRight", "ArrowRight", 39},
		"Home":       {"Home", "Home", 36},
		"End":        {"End", "End", 35},
		"PageUp":     {"PageUp", "PageUp", 33},
		"PageDown":   {"PageDown", "PageDown", 34},
		"a":          {"a", "KeyA", 65},
		"c":          {"c", "KeyC", 67},
		"v":          {"v", "KeyV", 86},
	}
	spec, ok := specs[key]
	if !ok {
		return errors.New("input key is not allowed")
	}
	mask := 0
	for _, modifier := range modifiers {
		switch strings.ToLower(strings.TrimSpace(modifier)) {
		case "alt":
			mask |= 1
		case "ctrl":
			mask |= 2
		case "meta":
			mask |= 4
		case "shift":
			mask |= 8
		}
	}
	params := map[string]any{
		"key":                   spec.key,
		"code":                  spec.code,
		"windowsVirtualKeyCode": spec.vk,
		"nativeVirtualKeyCode":  spec.vk,
		"modifiers":             mask,
	}
	down := map[string]any{"type": "rawKeyDown"}
	up := map[string]any{"type": "keyUp"}
	for name, value := range params {
		down[name] = value
		up[name] = value
	}
	if err := c.client.call(ctx, sessionID, "Input.dispatchKeyEvent", down); err != nil {
		return err
	}
	return c.client.call(ctx, sessionID, "Input.dispatchKeyEvent", up)
}

func (c *inputController) probeCaret(ctx context.Context, sessionID string) (*inputCaret, error) {
	result, err := c.client.callResult(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    caretProbeExpression,
		"returnByValue": true,
	})
	if err != nil {
		return nil, err
	}
	var evaluated struct {
		Result struct {
			Value *inputCaret `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &evaluated); err != nil {
		return nil, fmt.Errorf("parse caret probe: %w", err)
	}
	if len(evaluated.ExceptionDetails) > 0 && string(evaluated.ExceptionDetails) != "null" {
		return nil, errors.New("caret probe raised an exception")
	}
	if evaluated.Result.Value == nil {
		return nil, nil
	}
	if !validInputCaret(*evaluated.Result.Value) {
		return nil, errors.New("caret probe returned invalid geometry")
	}
	return evaluated.Result.Value, nil
}

func (c *inputController) writeState(state inputState) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if state.ID < c.lastWritten {
		return nil
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := writeNavigationStatusAtomic(c.filePath(inputStateFileName), payload); err != nil {
		return err
	}
	c.lastWritten = state.ID
	return nil
}

const caretProbeExpression = `(() => {
	const active = document.activeElement;
	if (!active || typeof active.getBoundingClientRect !== 'function') return null;
	const editable = active.isContentEditable ||
		active.matches('input:not([type="hidden"]), textarea, [contenteditable="true"], [role="textbox"]');
	if (!editable) return null;
	const activeRect = active.getBoundingClientRect();
	if (activeRect.width <= 0 || activeRect.height <= 0) return null;
	const normalize = (rect) => {
		if (!rect) return null;
		const x = rect.left;
		const y = rect.top;
		const width = Math.max(1, rect.width);
		const height = Math.max(1, rect.height || rect.bottom - rect.top);
		if (![x, y, width, height].every(Number.isFinite)) return null;
		return { x, y, width, height };
	};
	if (active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement) {
		const start = typeof active.selectionStart === 'number' ? active.selectionStart : (active.value || '').length;
		const mirror = document.createElement('div');
		const style = window.getComputedStyle(active);
		const mirrorStyle = mirror.style;
		mirrorStyle.position = 'absolute';
		mirrorStyle.visibility = 'hidden';
		mirrorStyle.whiteSpace = 'pre-wrap';
		mirrorStyle.overflowWrap = 'break-word';
		mirrorStyle.width = activeRect.width + 'px';
		mirrorStyle.height = activeRect.height + 'px';
		for (const property of [
			'fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'letterSpacing', 'lineHeight',
			'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft', 'borderTopWidth',
			'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth', 'boxSizing', 'textTransform'
		]) mirrorStyle[property] = style[property];
		mirror.textContent = (active.value || '').slice(0, start);
		const marker = document.createElement('span');
		marker.textContent = '\u200b';
		mirror.appendChild(marker);
		document.body.appendChild(mirror);
		const mirrorRect = mirror.getBoundingClientRect();
		const markerRect = marker.getBoundingClientRect();
		mirror.remove();
		const parsedLineHeight = parseFloat(style.lineHeight);
		const parsedFontSize = parseFloat(style.fontSize);
		return normalize({
			left: activeRect.left + markerRect.left - mirrorRect.left - (active.scrollLeft || 0),
			top: activeRect.top + markerRect.top - mirrorRect.top - (active.scrollTop || 0),
			width: markerRect.width,
			height: markerRect.height || (Number.isFinite(parsedLineHeight) ? parsedLineHeight : parsedFontSize) || 1,
		});
	}
	const selection = window.getSelection();
	if (selection && selection.rangeCount > 0) {
		const range = selection.getRangeAt(0).cloneRange();
		let rect = range.getBoundingClientRect();
		if (!rect || (!rect.width && !rect.height)) {
			const rects = range.getClientRects();
			rect = rects && rects.length ? rects[0] : activeRect;
		}
		return normalize(rect);
	}
	return normalize(activeRect);
})()`
