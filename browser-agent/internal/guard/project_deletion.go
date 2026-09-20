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
)

const (
	projectDeletionCommandFileName = "project-delete-command.json"
	projectDeletionStatusFileName  = "project-delete.json"
	projectDeletionPollInterval    = 300 * time.Millisecond
)

type projectDeletionCommand struct {
	ID          int64  `json:"id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	RequestedAt int64  `json:"requested_at"`
}

type projectDeletionStatus struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"project_id"`
	State     string `json:"state"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

type projectDeletionController struct {
	dir        string
	client     *cdpClient
	navigation *navigationController
	logger     *slog.Logger

	commandMu   sync.Mutex
	lastSeen    int64
	lastApplied int64
	writeMu     sync.Mutex
	lastWritten int64
}

func newProjectDeletionController(dir string, client *cdpClient, navigation *navigationController, logger *slog.Logger) *projectDeletionController {
	if logger == nil {
		logger = slog.Default()
	}
	controller := &projectDeletionController{dir: strings.TrimSpace(dir), client: client, navigation: navigation, logger: logger}
	if data, err := os.ReadFile(controller.filePath(projectDeletionStatusFileName)); err == nil {
		var status projectDeletionStatus
		if json.Unmarshal(data, &status) == nil && status.ID >= 0 {
			controller.lastSeen = status.ID
			controller.lastApplied = status.ID
			controller.lastWritten = status.ID
		}
	}
	return controller
}

func (c *projectDeletionController) filePath(name string) string {
	if c == nil || c.dir == "" {
		return ""
	}
	return filepath.Join(c.dir, name)
}

func (c *projectDeletionController) run(ctx context.Context) {
	ticker := time.NewTicker(projectDeletionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.processCommand(ctx); err != nil {
				c.logger.Debug("project deletion command failed",
					"event", "project_deletion_command_failed",
					"component", "guard",
					"error", err,
				)
			}
		}
	}
}

func (c *projectDeletionController) processCommand(ctx context.Context) error {
	if c == nil || c.client == nil {
		return nil
	}
	data, err := os.ReadFile(c.filePath(projectDeletionCommandFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var command projectDeletionCommand
	if err := json.Unmarshal(data, &command); err != nil || command.ID <= 0 || strings.TrimSpace(command.ProjectID) == "" {
		return nil
	}
	c.commandMu.Lock()
	if command.ID <= c.lastSeen {
		c.commandMu.Unlock()
		return nil
	}
	c.lastSeen = command.ID
	c.commandMu.Unlock()

	status := projectDeletionStatus{
		ID:        command.ID,
		ProjectID: command.ProjectID,
		State:     "FAILED",
		Error:     "ERR_PROJECT_DELETE_UI_NOT_FOUND",
		UpdatedAt: time.Now().Unix(),
	}
	sessionID := c.currentSessionID()
	if sessionID == "" {
		status.Error = "ERR_PROJECT_DELETE_UI_NOT_FOUND"
	} else if err := c.deleteProviderProject(ctx, sessionID, command.ProjectID, command.ProjectName); err != nil {
		if code := projectDeletionErrorCode(err); code != "" {
			status.Error = code
		}
	} else {
		status.State = "DONE"
		status.Error = ""
	}

	if err := c.writeStatus(status); err != nil {
		return err
	}
	c.commandMu.Lock()
	c.lastApplied = command.ID
	c.commandMu.Unlock()
	return nil
}

func (c *projectDeletionController) currentSessionID() string {
	if c == nil || c.navigation == nil {
		return ""
	}
	return c.navigation.currentSession()
}

func (c *projectDeletionController) deleteProviderProject(ctx context.Context, sessionID, projectID, projectName string) error {
	encodedID, err := json.Marshal(strings.TrimSpace(projectID))
	if err != nil {
		return err
	}
	encodedName, err := json.Marshal(strings.TrimSpace(projectName))
	if err != nil {
		return err
	}
	expression := fmt.Sprintf(projectDeletionExpression, string(encodedID), string(encodedName))
	result, err := c.client.callResult(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return err
	}
	var evaluated struct {
		Result struct {
			Value struct {
				Deleted bool   `json:"deleted"`
				Error   string `json:"error"`
			} `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &evaluated); err != nil {
		return fmt.Errorf("parse project deletion result: %w", err)
	}
	if len(evaluated.ExceptionDetails) > 0 && string(evaluated.ExceptionDetails) != "null" {
		return errors.New("ERR_PROJECT_DELETE_UI_NOT_FOUND")
	}
	if !evaluated.Result.Value.Deleted {
		if evaluated.Result.Value.Error != "" {
			return errors.New(evaluated.Result.Value.Error)
		}
		return errors.New("ERR_PROJECT_DELETE_UI_NOT_FOUND")
	}
	return nil
}

func projectDeletionErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	switch code {
	case "ERR_PROJECT_DELETE_UI_NOT_FOUND", "ERR_PROJECT_DELETE_TIMEOUT", "ERR_PROJECT_DELETE_FAILED":
		return code
	default:
		return "ERR_PROJECT_DELETE_FAILED"
	}
}

func (c *projectDeletionController) writeStatus(status projectDeletionStatus) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if status.ID < c.lastWritten {
		return nil
	}
	payload, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if err := writeNavigationStatusAtomic(c.filePath(projectDeletionStatusFileName), payload); err != nil {
		return err
	}
	c.lastWritten = status.ID
	return nil
}

const projectDeletionExpression = `(async () => {
	const projectID = %s;
	const projectName = String(%s).trim().toLowerCase();
	const visible = (el) => {
		if (!el) return false;
		const rect = el.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const norm = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const nameOf = (el) => norm(el.getAttribute('aria-label')) || norm(el.getAttribute('title')) || norm(el.innerText);
	const waitFor = async (predicate, timeout = 5000) => {
		const deadline = Date.now() + timeout;
		while (Date.now() < deadline) {
			const value = predicate();
			if (value) return value;
			await new Promise((resolve) => setTimeout(resolve, 100));
		}
		return null;
	};
	const matchingOptions = () => Array.from(document.querySelectorAll('button, [role="button"]')).filter((el) => {
		const label = norm(el.getAttribute('aria-label'));
		return label.includes('open project options') && label.includes(projectName);
	});
	const revealOptions = async () => {
		for (let attempt = 0; attempt < 4; attempt++) {
			let candidates = matchingOptions();
			if (candidates.length > 0) {
				candidates[0].scrollIntoView({ block: 'center', inline: 'nearest' });
				await new Promise((resolve) => setTimeout(resolve, 250));
				candidates = matchingOptions().filter(visible);
				if (candidates.length > 0) return candidates[0];
			}
			const named = Array.from(document.querySelectorAll('a, button, [role="link"], [role="button"], div')).filter((el) => {
				return nameOf(el) === projectName || norm(el.getAttribute('href')).includes(projectID);
			});
			for (const anchor of named) {
				let node = anchor;
				for (let depth = 0; depth < 7 && node; depth++, node = node.parentElement) {
					const candidate = node.querySelector && node.querySelector('button[aria-label*="Open project options"], [role="button"][aria-label*="Open project options"]');
					if (candidate) {
						candidate.scrollIntoView({ block: 'center', inline: 'nearest' });
						await new Promise((resolve) => setTimeout(resolve, 250));
						return candidate;
					}
				}
			}
			const showMore = Array.from(document.querySelectorAll('button, [role="button"]')).find((el) => visible(el) && /^(show more|显示更多)$/.test(nameOf(el)));
			if (!showMore) break;
			showMore.click();
			await new Promise((resolve) => setTimeout(resolve, 300));
		}
		return null;
	};
	const options = await revealOptions();
	if (!options) return { deleted: false, error: 'ERR_PROJECT_DELETE_UI_NOT_FOUND' };
	options.click();
	const menu = await waitFor(() => Array.from(document.querySelectorAll('[role="menuitem"], button, [role="button"]')).find((el) => {
		if (!visible(el)) return false;
		return /^(delete project|delete|删除项目|删除)$/.test(nameOf(el));
	}), 5000);
	if (!menu) return { deleted: false, error: 'ERR_PROJECT_DELETE_UI_NOT_FOUND' };
	menu.click();
	const confirm = await waitFor(() => {
		const dialogs = Array.from(document.querySelectorAll('[role="dialog"], [role="alertdialog"], dialog')).filter(visible);
		for (const dialog of dialogs) {
			const button = Array.from(dialog.querySelectorAll('button, [role="button"]')).filter(visible).find((el) => {
				return /^(delete project|delete|delete from chat and work|删除项目|删除|从聊天和工作删除|删除聊天和工作)$/.test(nameOf(el));
			});
			if (button) return button;
		}
		return Array.from(document.querySelectorAll('button, [role="button"]')).find((el) => {
			if (!visible(el)) return false;
			return /^(delete from chat and work|从聊天和工作删除|删除聊天和工作)$/.test(nameOf(el));
		}) || null;
	}, 5000);
	if (!confirm) return { deleted: false, error: 'ERR_PROJECT_DELETE_UI_NOT_FOUND' };
	confirm.click();
	const gone = await waitFor(() => matchingOptions().length === 0, 8000);
	if (!gone) return { deleted: false, error: 'ERR_PROJECT_DELETE_TIMEOUT' };
	return { deleted: true, error: '' };
})()`
