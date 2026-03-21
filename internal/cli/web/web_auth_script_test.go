package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadAppleTwoFactorScript(t *testing.T) string {
	t.Helper()

	scriptPath := filepath.Join("..", "..", "..", "scripts", "get-apple-2fa-code.scpt")
	contents, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	return string(contents)
}

func TestAppleTwoFactorScriptScansForCodeBeforeTrustClick(t *testing.T) {
	script := loadAppleTwoFactorScript(t)

	scanIndex := strings.Index(script, "set code to my scanWindowForCode(currentWindow)")
	clickIndex := strings.Index(script, "set didAdvanceTrustPrompt to my clickTrustButtonIfPresent(currentWindow)")
	if scanIndex == -1 || clickIndex == -1 {
		t.Fatalf("expected scan and trust-click flow in script")
	}
	if scanIndex > clickIndex {
		t.Fatalf("expected script to scan for a code before attempting to advance the trust dialog")
	}
}

func TestAppleTwoFactorScriptAllowsSingleButtonTrustDialogs(t *testing.T) {
	script := loadAppleTwoFactorScript(t)

	if strings.Contains(script, "if (count of windowButtons) < 2 then") {
		t.Fatalf("script still rejects single-button trust dialogs")
	}
	if !strings.Contains(script, "if (count of windowButtons) = 0 then") {
		t.Fatalf("expected script to allow one-button fallback when advancing trust dialogs")
	}
}

func TestAppleTwoFactorScriptUsesTwoStepDeadlineAssignment(t *testing.T) {
	script := loadAppleTwoFactorScript(t)

	if strings.Contains(script, "set deadlineAt to (current date) + timeoutSeconds") {
		t.Fatalf("expected conservative two-step deadline assignment for AppleScript compatibility")
	}
	if !strings.Contains(script, "set deadlineAt to (current date)") {
		t.Fatalf("expected deadline initialization in script")
	}
	if !strings.Contains(script, "set deadlineAt to deadlineAt + timeoutSeconds") {
		t.Fatalf("expected deadline extension in script")
	}
}
