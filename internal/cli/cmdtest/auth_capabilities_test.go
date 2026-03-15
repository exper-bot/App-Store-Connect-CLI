package cmdtest

import (
	"strings"
	"testing"

	cmd "github.com/rudrankriyam/App-Store-Connect-CLI/cmd"
)

func TestAuthCapabilitiesInvalidOutputReturnsExitUsage(t *testing.T) {
	_, stderr := captureOutput(t, func() {
		code := cmd.Run([]string{"auth", "capabilities", "--output", "yaml"}, "1.0.0")
		if code != cmd.ExitUsage {
			t.Fatalf("exit code = %d, want %d", code, cmd.ExitUsage)
		}
	})
	if !strings.Contains(stderr, "unsupported format: yaml") {
		t.Fatalf("expected stderr to contain unsupported format error, got %q", stderr)
	}
}

func TestAuthCapabilitiesUnexpectedArgumentReturnsExitUsage(t *testing.T) {
	_, stderr := captureOutput(t, func() {
		code := cmd.Run([]string{"auth", "capabilities", "extra"}, "1.0.0")
		if code != cmd.ExitUsage {
			t.Fatalf("exit code = %d, want %d", code, cmd.ExitUsage)
		}
	})
	if !strings.Contains(stderr, "unexpected argument(s): extra") {
		t.Fatalf("expected stderr to contain unexpected argument error, got %q", stderr)
	}
}
