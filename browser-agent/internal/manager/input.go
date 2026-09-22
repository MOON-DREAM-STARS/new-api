package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	inputStateName   = "input.json"
	inputCommandName = "input-command.json"

	inputPollInterval = 20 * time.Millisecond
	inputWaitTimeout  = 10 * time.Second
	inputTextMaxRunes = 4096

	inputActionText  = "input_text"
	inputActionKey   = "input_key"
	inputActionCaret = "caret_probe"

	inputStateDone   = "DONE"
	inputStateFailed = "FAILED"

	inputErrorUnavailable = "WEB_WORKSPACE_INPUT_UNAVAILABLE"
	inputErrorTimeout     = "WEB_WORKSPACE_INPUT_TIMEOUT"
	inputErrorRejected    = "WEB_WORKSPACE_INPUT_REJECTED"
)

var (
	ErrInputUnavailable = errors.New("web workspace input unavailable")
	ErrInputTimeout     = errors.New("web workspace input timeout")
	ErrInputRejected    = errors.New("web workspace input rejected")
)

// InputCaret is the remote browser caret rectangle in remote CSS pixels. It
// never contains a DOM node, CDP session or provider address.
type InputCaret struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type inputCommandFile struct {
	ID          int64    `json:"id"`
	Action      string   `json:"action"`
	Text        string   `json:"text,omitempty"`
	Key         string   `json:"key,omitempty"`
	Modifiers   []string `json:"modifiers,omitempty"`
	RequestedAt int64    `json:"requested_at"`
}

type inputStateFile struct {
	ID        int64       `json:"id"`
	Action    string      `json:"action"`
	State     string      `json:"state"`
	Error     string      `json:"error"`
	Caret     *InputCaret `json:"caret"`
	UpdatedAt int64       `json:"updated_at"`
}

func readInputState(root *os.Root) (inputStateFile, bool, error) {
	data, err := root.ReadFile(inputStateName)
	if errors.Is(err, os.ErrNotExist) {
		return inputStateFile{}, false, nil
	}
	if err != nil {
		return inputStateFile{}, false, err
	}
	var state inputStateFile
	if err := json.Unmarshal(data, &state); err != nil || !validInputStateFile(state) {
		return inputStateFile{}, false, nil
	}
	return state, true, nil
}

func validInputStateFile(state inputStateFile) bool {
	if state.ID < 0 || state.UpdatedAt < 0 {
		return false
	}
	switch state.Action {
	case inputActionText, inputActionKey, inputActionCaret:
	default:
		return false
	}
	switch state.State {
	case inputStateDone:
		return state.Error == "" && (state.Caret == nil || validInputCaret(*state.Caret))
	case inputStateFailed:
		return validInputErrorCode(state.Error) && state.Caret == nil
	default:
		return false
	}
}

func validInputErrorCode(code string) bool {
	switch code {
	case inputErrorUnavailable, inputErrorTimeout, inputErrorRejected:
		return true
	default:
		return false
	}
}

func validInputCaret(caret InputCaret) bool {
	return caret.X >= 0 && caret.Y >= 0 && caret.Width >= 0 && caret.Height >= 0 &&
		caret.X < 100000 && caret.Y < 100000 && caret.Width < 100000 && caret.Height < 100000
}

func readInputCommandID(root *os.Root) int64 {
	data, err := root.ReadFile(inputCommandName)
	if err != nil {
		return 0
	}
	var command inputCommandFile
	if json.Unmarshal(data, &command) != nil || command.ID <= 0 {
		return 0
	}
	return command.ID
}

func nextInputCommandID(root *os.Root) int64 {
	last := readInputCommandID(root)
	if state, ok, err := readInputState(root); err == nil && ok && state.ID > last {
		last = state.ID
	}
	return last + 1
}

// InputText injects one already-committed local text value into the remote
// browser's current focus. It never replays raw key events.
func (m *Manager) InputText(ctx context.Context, workspaceID int64, text string) error {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > inputTextMaxRunes {
		return fmt.Errorf("%w: text is empty or too long", ErrInputRejected)
	}
	_, err := m.runInputCommand(ctx, workspaceID, inputCommandFile{
		Action: inputActionText,
		Text:   text,
	})
	return err
}

// InputKey forwards one approved key with modifiers to the remote browser.
func (m *Manager) InputKey(ctx context.Context, workspaceID int64, key string, modifiers []string) error {
	normalizedKey, normalizedModifiers, err := normalizeInputKey(key, modifiers)
	if err != nil {
		return err
	}
	_, err = m.runInputCommand(ctx, workspaceID, inputCommandFile{
		Action:    inputActionKey,
		Key:       normalizedKey,
		Modifiers: normalizedModifiers,
	})
	return err
}

// InputCaret probes the remote viewport for the current caret. A nil result is
// valid and means no active editable element was found.
func (m *Manager) InputCaret(ctx context.Context, workspaceID int64) (*InputCaret, error) {
	state, err := m.runInputCommand(ctx, workspaceID, inputCommandFile{Action: inputActionCaret})
	if err != nil {
		return nil, err
	}
	return state.Caret, nil
}

func (m *Manager) runInputCommand(ctx context.Context, workspaceID int64, command inputCommandFile) (inputStateFile, error) {
	if workspaceID <= 0 {
		return inputStateFile{}, fmt.Errorf("%w: workspace id is invalid", ErrInvalidRequest)
	}
	feature := m.featureLock(featureInput, workspaceID)
	feature.Lock()
	defer feature.Unlock()

	root, command, err := m.prepareInputCommand(workspaceID, command)
	if err != nil {
		return inputStateFile{}, err
	}
	defer root.Close()
	return m.waitInputResult(ctx, root, command)
}

// prepareInputCommand holds the lifecycle lock only for the runtime check and
// command-file write. The caller waits for the guard receipt after releasing
// that lock so unrelated workspace operations stay responsive.
func (m *Manager) prepareInputCommand(workspaceID int64, command inputCommandFile) (*os.Root, inputCommandFile, error) {
	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()

	state, err := m.liveRuntimeState(workspaceID)
	if err != nil {
		return nil, command, err
	}
	if state != StateRunning && state != StateIdle {
		return nil, command, fmt.Errorf("%w: runtime is %s", ErrInputUnavailable, state)
	}

	root, err := ensureGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID), m.chown)
	if err != nil {
		return nil, command, fmt.Errorf("%w: prepare guard state directory: %v", ErrInputUnavailable, err)
	}
	command.ID = nextInputCommandID(root)
	command.RequestedAt = m.now().Unix()
	payload, err := json.Marshal(command)
	if err != nil {
		_ = root.Close()
		return nil, command, fmt.Errorf("%w: encode input command: %v", ErrInputUnavailable, err)
	}
	if err := writeStateFileAtomic(root, inputCommandName, payload); err != nil {
		_ = root.Close()
		return nil, command, fmt.Errorf("%w: write input command: %v", ErrInputUnavailable, err)
	}
	return root, command, nil
}

func (m *Manager) waitInputResult(ctx context.Context, root *os.Root, command inputCommandFile) (inputStateFile, error) {
	deadline := time.Now().Add(inputWaitTimeout)
	ticker := time.NewTicker(inputPollInterval)
	defer ticker.Stop()
	for {
		state, ok, err := readInputState(root)
		if err != nil {
			return inputStateFile{}, fmt.Errorf("%w: read input state: %v", ErrInputUnavailable, err)
		}
		if ok && state.ID == command.ID {
			switch state.State {
			case inputStateDone:
				if state.Action != command.Action {
					return inputStateFile{}, fmt.Errorf("%w: input action mismatch", ErrInputUnavailable)
				}
				return state, nil
			case inputStateFailed:
				return inputStateFile{}, inputStateError(state.Error)
			}
		}
		if !time.Now().Before(deadline) {
			return inputStateFile{}, ErrInputTimeout
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return inputStateFile{}, ErrInputTimeout
			}
			return inputStateFile{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func inputStateError(code string) error {
	switch code {
	case inputErrorTimeout:
		return ErrInputTimeout
	case inputErrorRejected:
		return ErrInputRejected
	default:
		return ErrInputUnavailable
	}
}

func normalizeInputKey(key string, modifiers []string) (string, []string, error) {
	key = strings.TrimSpace(key)
	switch key {
	case "Esc":
		key = "Escape"
	case "A":
		key = "a"
	case "C":
		key = "c"
	case "V":
		key = "v"
	}
	switch key {
	case "Enter", "Backspace", "Delete", "Escape", "Tab", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
		"Home", "End", "PageUp", "PageDown", "a", "c", "v":
	default:
		return "", nil, fmt.Errorf("%w: key is not allowed", ErrInputRejected)
	}
	normalized := make([]string, 0, len(modifiers))
	seen := make(map[string]struct{}, len(modifiers))
	for _, modifier := range modifiers {
		modifier = strings.ToLower(strings.TrimSpace(modifier))
		switch modifier {
		case "ctrl", "shift", "alt", "meta":
		default:
			return "", nil, fmt.Errorf("%w: modifier is not allowed", ErrInputRejected)
		}
		if _, ok := seen[modifier]; ok {
			return "", nil, fmt.Errorf("%w: duplicate modifier", ErrInputRejected)
		}
		seen[modifier] = struct{}{}
		normalized = append(normalized, modifier)
	}
	if len(normalized) > 4 {
		return "", nil, fmt.Errorf("%w: too many modifiers", ErrInputRejected)
	}
	if key == "a" || key == "c" || key == "v" {
		if _, ok := seen["ctrl"]; !ok {
			if _, ok := seen["meta"]; !ok {
				return "", nil, fmt.Errorf("%w: %s requires ctrl or meta", ErrInputRejected, key)
			}
		}
	}
	return key, normalized, nil
}
