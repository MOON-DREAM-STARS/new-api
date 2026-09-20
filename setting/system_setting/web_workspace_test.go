package system_setting

import "testing"

func TestNormalizeWebWorkspaceScreenBounds(t *testing.T) {
	settings := &WebWorkspaceSettings{
		MaxScreenWidth:  2048,
		MaxScreenHeight: 900,
	}
	width, height := NormalizeWebWorkspaceScreenBounds(settings)
	if width != 2048 || height != 900 {
		t.Fatalf("expected 2048x900, got %dx%d", width, height)
	}

	settings.MaxScreenWidth = 1
	settings.MaxScreenHeight = 9999
	width, height = NormalizeWebWorkspaceScreenBounds(settings)
	if width != WebWorkspaceMaxScreenWidth || height != WebWorkspaceMaxScreenHeight {
		t.Fatalf("expected defaults %dx%d, got %dx%d", WebWorkspaceMaxScreenWidth, WebWorkspaceMaxScreenHeight, width, height)
	}
}
