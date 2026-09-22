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

func TestProjectDeletionExpressionMatchesNormalizedProviderLabels(t *testing.T) {
	assert.Contains(t, projectDeletionExpression, "String(%s).trim().toLowerCase()")
	assert.Contains(t, projectDeletionExpression, "delete from chat and work")
}

// The runtime forces --lang=zh-CN, so the provider renders "打开 <name> 的项目选项"
// and "删除项目". A probe that only matched English failed closed with
// ERR_PROJECT_DELETE_UI_NOT_FOUND; both languages must stay accepted.
func TestProjectDeletionExpressionAcceptsSimplifiedChineseLabels(t *testing.T) {
	assert.Contains(t, projectDeletionExpression, "项目选项")
	assert.Contains(t, projectDeletionExpression, "删除项目")
	assert.Contains(t, projectDeletionExpression, "显示更多")

	// The option button is only locatable if the attribute selector also carries
	// the Chinese wording, because the live aria-label is Chinese.
	assert.Contains(t, projectDeletionExpression, "[aria-label*=\"项目选项\"]")
}

func TestProjectDeletionWritesCommandReceipt(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")

	f.queueResult("Runtime.evaluate", map[string]any{
		"result": map[string]any{"type": "object", "value": map[string]any{"deleted": true, "error": ""}},
	})
	writeStateFile(t, dir, projectDeletionCommandFileName, map[string]any{
		"id":           1,
		"project_id":   testProjectID,
		"project_name": "test0",
		"requested_at": time.Now().Unix(),
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(dir, projectDeletionStatusFileName))
		if err == nil {
			var status projectDeletionStatus
			require.NoError(t, json.Unmarshal(data, &status))
			assert.Equal(t, int64(1), status.ID)
			assert.Equal(t, testProjectID, status.ProjectID)
			assert.Equal(t, "DONE", status.State)
			assert.Empty(t, status.Error)
			run.assertRunning(100 * time.Millisecond)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for project deletion receipt: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
